<p align="center">
  <img src="ai.ico" alt="Gopher AI icon" width="96" />
</p>

# Gopher AI

Gopher AI is a local-first AI assistant built mainly in Go.

It gives you:

- a desktop app backend with an embedded web UI
- a signed Android release APK
- local inference through `llama.cpp` with `llama-server`
- automatic Gemini fallback
- automatic LoRA training queue
- manual training from the app UI
- JSON-based local storage in `~/.gopher-ai/`

## Main model

The default model shown in the chat composer is:

```text
Gopher-AI
```

`Gopher-AI` is the product model and routing layer.

It is not a separate foundation model file. It means:

1. try your local `llama.cpp` model first
2. keep the conversation local whenever possible
3. fall back to Gemini when local inference is unavailable or times out
4. apply your trained LoRA adapters back into the local runtime

## Build outputs

The release bundle is already generated in `build/`:

- `build/linux/gopher-ai`
- `build/linux/Gopher-AI-Installer.sh`
- `build/linux/Gopher-AI-Linux-Package.7z`
- `build/windows/gopher-ai.exe`
- `build/windows/Gopher-AI-Installer.ps1`
- `build/windows/Gopher-AI-Installer.bat`
- `build/windows/Gopher-AI-Windows-Package.7z`
- `build/mobile/Gopher-AI-debug.apk`
- `build/mobile/Gopher-AI-release.apk`

To regenerate everything:

```bash
GOCACHE=/tmp/go-build-cache ./scripts/build_release.sh
```

## Installers

### Linux installer

Run:

```bash
./build/linux/Gopher-AI-Installer.sh
```

It opens a desktop setup wizard with:

- a left-side Gopher AI artwork panel
- a classic wizard layout with welcome, options, install, and finish steps
- user-space install by default, so it does not need administrator access
- clickable options for `PATH`, desktop shortcut, Python training packages, and launch-after-install
- optional app-local Python runtime for training inside the install folder

### Windows installer

Use:

- `build/windows/Gopher-AI-Installer.ps1`
- or `build/windows/Gopher-AI-Installer.bat`

The Windows installer now uses the same wizard UI and includes:

- install folder picker
- `Add Gopher AI to PATH`
- `Create desktop shortcut`
- `Install Python training dependencies`
- `Launch Gopher AI after installation`
- a classic setup-style window with Gopher AI art on the left
- optional app-local Python runtime for training

## Quick start

### Linux

```bash
export GEMINI_API_KEY="your_key_here"
./build/linux/gopher-ai
```

### Windows

```powershell
$env:GEMINI_API_KEY="your_key_here"
.\build\windows\gopher-ai.exe
```

Then open:

```text
http://127.0.0.1:8080
```

### Android

Install:

- `build/mobile/Gopher-AI-release.apk`

The app name is `Gopher AI` and it uses the root icon `ai.ico` converted into launcher assets.

## Local inference

By default Gopher AI expects `llama-server` on:

```text
http://127.0.0.1:8081
```

Important config lives in:

```text
~/.gopher-ai/config.json
```

Main fields:

- `models.primary`
- `models.fallback`
- `models.fallback_secondary`
- `llama.enabled`
- `llama.serverUrl`
- `llama.autoStart`
- `llama.binaryPath`
- `llama.modelPath`
- `llama.activeAdapterPath`

Default routing:

- primary: `gopher-ai`
- fallback: `gemini-3.1-pro-preview`
- secondary fallback: `gemini-3-flash-preview`

## How the product works

1. You open Gopher AI on desktop or Android.
2. The desktop server loads chats, config, quota tracking, attachments, and training state from `~/.gopher-ai/`.
3. When you send a message, the selected default model is `Gopher-AI`.
4. `Gopher-AI` first tries the local `llama.cpp` runtime.
5. If local inference fails or times out, it falls back to Gemini.
6. The final answer is stored in the chat JSON.
7. When enough user/assistant pairs exist, Gopher AI can queue a LoRA training task.
8. The trainer writes an adapter.
9. Gopher AI points `llama-server` to the new adapter and reloads it.

Training does **not** rebuild the executable.

It trains a LoRA adapter and plugs it into the local model runtime.

## Automatic training

Automatic training flow:

1. chat messages reach `training.minPairsToTrain`
2. a training task is queued
3. the dataset is exported to `~/.gopher-ai/training/datasets/<taskId>.jsonl`
4. `scripts/train_lora.py` runs
5. the adapter is written to `~/.gopher-ai/training/adapters/<taskId>/adapter.safetensors`
6. Gopher AI auto-applies that adapter to `llama-server`

If a run fails, logs go to:

```text
~/.gopher-ai/training/logs/<taskId>.log
```

## Manual training from the app

The desktop app now includes a training UI.

Open any chat, then click:

```text
Training
```

or:

```text
Train Chat
```

From there you can:

- choose the base model
- set epochs
- set learning rate
- set batch size
- start a manual training run for the current chat
- inspect the training queue

## Manual training from CLI

You can still train manually through the API.

### Create a manual task

```bash
curl -X POST http://127.0.0.1:8080/api/training/manual \
  -H "Content-Type: application/json" \
  -d '{
    "chatIds": ["chat_id_here"],
    "epochs": 3,
    "learningRate": 0.0001,
    "batchSize": 1,
    "baseModel": "mistralai/Mistral-7B-Instruct-v0.3"
  }'
```

### Check status

```bash
curl http://127.0.0.1:8080/api/training/status
```

### Download the dataset

```bash
curl http://127.0.0.1:8080/api/training/tasks/<taskId>/dataset
```

### Apply an external adapter

```bash
curl -X POST http://127.0.0.1:8080/api/training/tasks/<taskId>/apply \
  -H "Content-Type: application/json" \
  -d '{
    "adapterPath": "/absolute/path/to/adapter.safetensors"
  }'
```

## Python dependencies for training

Auto and manual LoRA training expect:

```bash
python3 -m pip install torch transformers peft
```

The desktop installers can install those packages for you as an optional step.

When you enable that option, the installer creates a local Python runtime inside the Gopher AI install folder and uses it for LoRA training.

## Android build

Android now builds with:

- local SDK under `.android-sdk/`
- local Gradle 8.9 under `.gradle-dist/gradle-8.9/`
- local signing material under `.signing/`

Build again with:

```bash
./scripts/build_android.sh
```

This produces:

- `build/mobile/Gopher-AI-debug.apk`
- `build/mobile/Gopher-AI-release.apk`

## API

Implemented endpoints:

- `GET /api/app/bootstrap`
- `GET /api/system/health`
- `GET /api/system/quota`
- `GET /api/models`
- `GET /api/training/status`
- `POST /api/training/manual`
- `GET /api/training/tasks/{taskID}/dataset`
- `POST /api/training/tasks/{taskID}/apply`
- `POST /api/chats`
- `GET /api/chats`
- `GET /api/chats/{chatID}`
- `PUT /api/chats/{chatID}`
- `DELETE /api/chats/{chatID}`
- `POST /api/chats/{chatID}/messages`
- `POST /api/attachments/upload`
- `GET /api/attachments/{attachmentID}`
- `DELETE /api/attachments/{attachmentID}`

## Storage

Default storage root:

```text
~/.gopher-ai/
```

Key paths:

- `config.json`
- `quota_tracking.json`
- `chats/`
- `trash/`
- `training/training_queue.json`
- `training/datasets/`
- `training/adapters/`
- `training/logs/`
- `attachments/temp/`
- `attachments/meta/`

Override the data directory with:

```bash
export GOPHER_AI_HOME=/some/path
```
