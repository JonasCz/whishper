from dotenv import load_dotenv
from fastapi import FastAPI, UploadFile, File
from models import ModelSize, Languages, DeviceType
from transcribe import transcribe_file, transcribe_from_filename, transcribe_from_url
import uvicorn
import os
from enum import Enum
from typing import Annotated
from backends.whisperx import WhisperxBackend
import subprocess
from fastapi import BackgroundTasks

app = FastAPI()

jobs = { 'status': 'idle', 'result': None }
    
@app.get("/transcribe/")
async def transcribe_endpoint(background_tasks: BackgroundTasks,
                              model_size: ModelSize = ModelSize.small, 
                              language: Languages = Languages.auto,
                              device: str = "cpu",
                              uploaded_file_url: str = None):
    
    if device != "cpu" and device != "cuda":
        return {"detail": "Device must be either cpu or cuda"}
    
    jobs['status'] = 'processing'
    jobs['result'] = None
    # Schedule the transcription task to run in the background
    background_tasks.add_task(long_running_task, model_size, device, uploaded_file_url, language)
    return {"status": "submitted"}
    
async def long_running_task(model_size, device, uploaded_file_url, language):
    print(f"Transcribing with model {model_size.value} on device {device}...")
    if uploaded_file_url is not None:
        # if a file url is provided, use it
        print(f"Transcribing from url: {uploaded_file_url}, for language {language.value}, model {model_size.value}, device {device}")
        jobs['result'] = await transcribe_from_url(uploaded_file_url, model_size.value, language.value, device)
        jobs['status'] = 'completed'
    else:
        return {"detail": "No file uploaded and no filename provided"}
    
@app.get("/poll/")
async def poll_endpoint():
    return jobs["status"]

@app.get("/get_result/")
async def get_result():
    return jobs['result']

@app.get("/healthcheck/")
async def healthcheck():
    return {"status": "healthy"}

if __name__ == "__main__":
    load_dotenv()

    # Get model list (comma separated) from environment variable
    model_list = os.environ.get("WHISPER_MODELS", "large-v3")
    model_list = model_list.split(",")
    for model in model_list:
        m = WhisperxBackend(model_size=model)
        m.get_model()
    
    #safeguard to stop pod after 10 minutes
    command = 'bash -c "sleep 600; runpodctl stop pod $RUNPOD_POD_ID"'
    process = subprocess.Popen(command, shell=True)

    uvicorn.run(app, host="0.0.0.0", port=8000)