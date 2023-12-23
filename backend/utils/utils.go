package utils

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"net/url"

	"github.com/rs/zerolog/log"
	"github.com/wader/goutubedl"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"codeberg.org/pluja/whishper/models"
)

func SanitizeFilename(filename string) string {
	// First remove trailing spaces
	filename = strings.TrimSpace(filename)
	// Then remove quotes and dots
	filename = strings.Trim(filename, `"'.`)
	reg, _ := regexp.Compile("[^a-zA-Z0-9]+")
	filename = reg.ReplaceAllString(filename, "_")
	return filename
}

func DownloadMedia(t *models.Transcription) (string, error) {
	if t.SourceUrl == "" {
		log.Debug().Msg("Source URL is empty")
		return "", fmt.Errorf("source URL is empty")
	}

	if t.ID == primitive.NilObjectID {
		log.Debug().Msg("Transcription ID is empty")
		return "", fmt.Errorf("transcription ID is empty")
	}

	goutubedl.Path = "yt-dlp"
	result, err := goutubedl.New(context.Background(), t.SourceUrl, goutubedl.Options{})
	if err != nil {
		log.Debug().Err(err).Msg("Error creating goutubedl")
		return "", err
	}

	downloadResult, err := result.Download(context.Background(), "best")
	if err != nil {
		log.Debug().Err(err).Msg("Error downloading media")
		return "", err
	}

	filename := fmt.Sprintf("%v%v%v", t.ID.Hex(), models.FileNameSeparator, result.Info.Title)
	filename = SanitizeFilename(filename)

	defer downloadResult.Close()
	f, err := os.Create(filepath.Join(os.Getenv("UPLOAD_DIR"), filename))
	if err != nil {
		log.Debug().Err(err).Msg("Error creating file")
		return "", err
	}
	defer f.Close()
	io.Copy(f, downloadResult)

	return filename, nil
}

func StopPodAndSleep() {
    // Call command
    cmd := exec.Command("runpodctl", "stop", "pod", os.Getenv("POD_ID"))
    var out bytes.Buffer
    cmd.Stdout = &out
    err := cmd.Run()
    if err != nil {
        fmt.Println(err)
        return
    }

    // Check output
    fmt.Println("Command executed, sleeping for 25 seconds...")
    time.Sleep(25 * time.Second)
}


type RequestBody struct {
    Query string `json:"query"`
}

type ResponseBody struct {
    Data struct {
        Pod struct {
            Machine struct {
                GpuAvailable int `json:"gpuAvailable"`
            } `json:"machine"`
        } `json:"pod"`
    } `json:"data"`
}

func SendTranscriptionRequest(t *models.Transcription) (*models.WhisperResult, error) {
	check_url := "https://api.runpod.io/graphql?api_key=" + os.Getenv("RUNPOD_API_KEY")
	log.Debug().Msgf("Checking if GPU is available at: %v", check_url)
    requestBody := &RequestBody{
        Query: `query Pod { pod(input: {podId: "dorrfi4dzlxcgb"}) { machine { gpuAvailable } } }`,
    }

    for {
        body, _ := json.Marshal(requestBody)
        req, err := http.NewRequest("POST", check_url, bytes.NewBuffer(body))
        if err != nil {
            time.Sleep(10 * time.Second)
            continue
        }
        req.Header.Set("Content-Type", "application/json")
        
        client := &http.Client{}
        resp, err := client.Do(req)
        if err != nil || resp.StatusCode != http.StatusOK {
            time.Sleep(10 * time.Second)
            continue
        }

        defer resp.Body.Close()

        respBody := new(ResponseBody)
        err = json.NewDecoder(resp.Body).Decode(respBody)
        if err != nil {
            time.Sleep(10 * time.Second)
            continue
        }

		log.Debug().Msgf("GPU available: %v", respBody.Data.Pod.Machine.GpuAvailable)
        
        if respBody.Data.Pod.Machine.GpuAvailable > 0 {
			log.Debug().Msg("GPU available, continuing...")
            // continue the function
            break
        }

		log.Debug().Msg("No GPU available, sleeping for 10 seconds...")
        time.Sleep(10 * time.Second)
    }

	//add file URL, at env(WHISHPER_HOST)/api/vido/ + filename
	uploaded_file_url := os.Getenv("WHISHPER_HOST") + "/api/video/" + t.FileName
	urlencoded_uploaded_file_url := url.QueryEscape(uploaded_file_url)

	log.Debug().Msgf("Serving uploaded video at: %v", uploaded_file_url)

	for {
        // Call command
        cmd := exec.Command("runpodctl", "start", "pod", os.Getenv("POD_ID"))
        var out bytes.Buffer
        cmd.Stdout = &out
        err := cmd.Run()
        if err != nil {
            fmt.Println(err)
			fmt.Println(out.String())
			fmt.Println("Error occurred, trying again in 25 seconds...")
			time.Sleep(25 * time.Second)
            continue
        }

        // Check output
        output := out.String()
        if strings.Contains(output, "Error: Something went wrong") {
            fmt.Println("Error occurred, trying again in 25 seconds...")
            time.Sleep(25 * time.Second)
            continue
        }
        if strings.Contains(output, "started") {
            fmt.Println("Pod started, sleeping for 25 seconds...")
            time.Sleep(25 * time.Second)
            break
        }
    }

	url := fmt.Sprintf("https://%v/transcribe/?model_size=%v&task=%v&language=%v&device=%v&uploaded_file_url=%v", os.Getenv("ASR_ENDPOINT"), t.ModelSize, t.Task, t.Language, t.Device, urlencoded_uploaded_file_url)
	log.Debug().Msgf("Sending initial transcription request to %v", url)
	// Send transcription request to transcription service
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		log.Debug().Err(err).Msg("Error creating request to transcription service")
		StopPodAndSleep()
		return nil, err
	}

	req.Header.Set("Accept", "application/json")
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		log.Debug().Err(err).Msg("Error sending request")
		StopPodAndSleep()
		return nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Debug().Err(err).Msg("Error reading response body")
		StopPodAndSleep()
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		log.Debug().Msgf("Response from %v: %v", url, string(b))
		log.Debug().Err(err).Msgf("Invalid response status %v:", resp.StatusCode)
		StopPodAndSleep()
		return nil, errors.New("invalid status")
	}

	
	for {
		status_poll_url := fmt.Sprintf("https://%v/poll/", os.Getenv("ASR_ENDPOINT"))
		log.Debug().Msgf("Polling status from: %v", status_poll_url)
		statusReq, err := http.NewRequest("GET", status_poll_url, nil)
		if err != nil {
            log.Debug().Err(err).Msg("Error creating request to transcription service poll endpoint")
            StopPodAndSleep()
            return nil, err
        }
        statusReq.Header.Set("Accept", "application/json")
        statusResp, err := client.Do(statusReq)
        if err != nil {
            log.Debug().Err(err).Msg("Error sending request to transcription service poll endpoint")
            StopPodAndSleep()
            return nil, err
        }
		defer resp.Body.Close()
        status_body, err := io.ReadAll(statusResp.Body)
        if err != nil {
            log.Debug().Err(err).Msg("Error reading response body from transcription service poll endpoint")
            StopPodAndSleep()
            return nil, err
        }
		status_body_string_without_quotes := strings.Trim(string(status_body), "\"")
		if status_body_string_without_quotes == "completed" {
			break
		} else {
			//524 response code = keep polling, it's working
			if statusResp.StatusCode == 524 {
				log.Debug().Msgf("Response from %v, timeout 524, continuing", status_poll_url)
				continue
			} else {
				log.Debug().Msgf("Response from %v: %v", status_poll_url, status_body_string_without_quotes)
				log.Debug().Err(err).Msgf("Invalid response status code %v, content: ", statusResp.StatusCode, status_body_string_without_quotes)
				StopPodAndSleep()
				return nil, errors.New("invalid status")
			}
		}
		time.Sleep(5 * time.Second) // wait for 5 seconds before next polling
	}

	url_get_result := fmt.Sprintf("https://%v/get_result/", os.Getenv("ASR_ENDPOINT"))
	log.Debug().Msgf("Requesting result from: %v", url_get_result)
	// Send transcription request to transcription service
	req_get_result, err := http.NewRequest("GET", url_get_result, nil)
	if err != nil {
		log.Debug().Err(err).Msg("Error creating request for result from transcription service")
		StopPodAndSleep()
		return nil, err
	}

	req_get_result.Header.Set("Accept", "application/json")
	client_get_result := &http.Client{}
	resp_get_result, err := client_get_result.Do(req_get_result)
	if err != nil {
		log.Debug().Err(err).Msg("Error sending request for result from transcription service")
		StopPodAndSleep()
		return nil, err
	}
	defer resp_get_result.Body.Close()
	transcription_response_body, err := io.ReadAll(resp_get_result.Body)
	if err != nil {
		log.Debug().Err(err).Msg("Error reading response body for result from transcription service")
		StopPodAndSleep()
		return nil, err
	}

	if resp_get_result.StatusCode != http.StatusOK {
		log.Debug().Msgf("Response from %v: %v", url_get_result, string(transcription_response_body))
		log.Debug().Err(err).Msgf("Invalid response status from transcription service %v:", resp_get_result.StatusCode)
		StopPodAndSleep()
		return nil, errors.New("invalid status")
	}

	var asrResponse *models.WhisperResult
	if err := json.Unmarshal(transcription_response_body, &asrResponse); err != nil {
		log.Debug().Err(err).Msg("Error decoding response")
		StopPodAndSleep()
		return nil, err
	}

	StopPodAndSleep()
	return asrResponse, nil

}
