# Web UI

## Overview

This is a minimal web installer interface for Kairos. It uses vanilla HTML, CSS, and JavaScript with no external dependencies.

## Features

- Cloud-config YAML editor with validation
- Device selection
- Installation options (reboot/power-off)
- Real-time installation progress via WebSocket
- Error message display

## Files

- `index.html` - Main installation form
- `progress.html` - Installation progress page with WebSocket streaming
- `message.html` - Error/success message display
- `favicon.ico` - Favicon

All files are embedded into the Go binary using `//go:embed` in `webui.go`.
