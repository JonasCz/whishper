package monitor

import (
	"github.com/rs/zerolog/log"

	"codeberg.org/pluja/whishper/api"
	"codeberg.org/pluja/whishper/models"
	"codeberg.org/pluja/whishper/utils"
)

func StartMonitor(s *api.Server) {
	log.Info().Msg("Starting monitor!")
	go func() {
		for {
			// Wait for new transcription to be added to the database
			// notification will be received through the NewTranscriptionCh channel
			<-s.NewTranscriptionCh
			pendingTranscriptions := s.Db.GetPendingTranscriptions()
			log.Debug().Msgf("Pending transcriptions: %v", len(pendingTranscriptions))
			for _, pt := range pendingTranscriptions {
				log.Debug().Msgf("Taking pending transcription %v", pt.ID)
				if pt.Status == models.TranscriptionStatusPending {
					err := transcribe(s, pt)
					if err != nil {
						log.Error().Err(err).Msg("Error transcribing")
						pt.Status = models.TranscriptionStatusError
						ut, err := s.Db.UpdateTranscription(pt)
						if err != nil {
							log.Error().Err(err).Msg("Error updating transcription")
						}
						s.BroadcastTranscription(ut)
						continue
					}
				}
			}
		}
	}()
}

func transcribe(s *api.Server, t *models.Transcription) error {
	// Update transcription status
	t.Status = models.TranscriptionStatusRunning
	log.Debug().Msgf("Updating transcription %v", t)
	_, err := s.Db.UpdateTranscription(t)
	if err != nil {
		log.Error().Err(err).Msg("Error updating transcription")
		return err
	}
	s.BroadcastTranscription(t)

	if t.SourceUrl != "" {
		// Download media
		fn, err := utils.DownloadMedia(t)
		if err != nil {
			log.Error().Err(err).Msg("Error downloading media")
			return err
		}
		t.FileName = fn
		s.BroadcastTranscription(t)
	}

	// Send transcription request to transcription service
	res, err := utils.SendTranscriptionRequest(t)
	if err != nil {
		log.Error().Err(err).Msg("Error sending transcription request")
		return err
	}

	t.Result = *res
	t.Translations = []models.Translation{}
	t.Status = models.TranscriptionStatusDone
	_, err = s.Db.UpdateTranscription(t)
	if err != nil {
		log.Error().Err(err).Msg("Error updating transcription")
		return err
	}
	s.BroadcastTranscription(t)
	return nil
}