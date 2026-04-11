# Project Orb

An AI-driven personal operating system that synthesizes your private files into a conversational life-management coach.

## What You Need

To use Project Orb, you need:

- the `project-orb` app binary
- a local OpenAI-compatible AI server running at `http://localhost:8080`

## Create Your Persona

The first time you run Project Orb, it creates a starter persona file for you automatically in your user config folder.

You can edit that file any time to make the coach feel more like you want.

The app uses XDG-style config, data, and state directories on macOS and Linux.

That means:

- config and persona live in the config directory
- analysis sessions live in app data under the analysis sessions subtree
- logs live in the state directory

## Start Your Local AI Server

Before opening the app, make sure your local AI server is running at:

```text
http://localhost:8080
```

The server must support:

- `POST /v1/chat/completions`
- streaming responses in SSE format

## Run The App

Open a terminal and run:

```bash
./project-orb
```
