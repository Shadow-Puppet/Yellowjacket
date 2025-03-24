# YellowJacket

A MusicBee inspired music player and library manager.

## Install

TODO: put release links here

## Features

TODO: add feature list and screenshots here

## Development

YellowJacket relies on [Wails](https://wails.io/docs/introduction) for development.
Wails allows us to build a frontend with HTML/CSS/JS and backend with Golang.

### Prerequisites

1. First, you will need Go installed.

2. Then, you will need the `wails` cli tool.

    Install it with

    ```shell
    go install github.com/wailsapp/wails/v2/cmd/wails@latest
    ```

3. After installing the Wails CLI you may need some build dependencies.
    View the missing Wails dependencies by running `wails doctor`.
    Using your system's package manager, install the missing dependencies.

### Dev Server

To run the Wails hot-reloading dev server, run `make dev` in the root of the project.
