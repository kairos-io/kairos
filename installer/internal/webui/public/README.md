# Web UI

## Overview

This is the web installer for Kairos. It is a step-by-step wizard written in
plain HTML, CSS and JavaScript, with no build step and nothing loaded from the
network.

## Features

- One step per screen: disk, user, SSH keys, hostname, timezone and keyboard,
  extensions, provider settings and what to do when the install finishes
- The same steps as the terminal installer, read from `GET /api/wizard`
- Every answer is checked on the server by `POST /api/step/:id`
- A review screen with the generated cloud-config, which can be edited by hand
  and is checked by `POST /validate-json`
- A confirmation of the disk that will be erased before Install is enabled
- Installation progress over WebSocket
- Error message display

## Files

- `index.html` - The wizard page and its styles
- `wizard.js` - The wizard: draws each step and talks to the JSON endpoints
- `progress.html` - Installation progress page with WebSocket streaming
- `message.html` - Error/success message display
- `favicon.ico` - Favicon

All files are embedded into the Go binary using `//go:embed` in `webui.go`.
