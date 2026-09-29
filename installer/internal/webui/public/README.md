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
  listed below
- `progress.html` - Installation progress page with WebSocket streaming
- `message.html` - Error/success message display
- `favicon.ico` - Favicon

## Endpoints wizard.js calls

- `GET /api/wizard` - the steps, with the disks and extensions found on the
  machine, and `advanced_disabled`, which makes the review read only
- `POST /api/step/:id` - checks one step's values; answers with the updated
  answers, or with one error per field
- `POST /api/render` - builds the cloud-config from the answers, for the
  review screen and for its "Regenerate from answers" button
- `POST /validate-json` - checks the cloud-config on the review screen against
  the schema
- `POST /install` - starts the install with a JSON body of `cloud_config`,
  `device` and `finish_action`, then the page moves to `progress.html`

`progress.html` reads the install progress from `GET /ws`.

All files are embedded into the Go binary using `//go:embed` in `webui.go`.
