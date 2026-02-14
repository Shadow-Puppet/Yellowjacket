# YellowJacket

How music was meant to bee.

## Install

You can grab the latest release [here](https://github.com/LJ-Software/yellowjacket/releases/latest)

## Features

TODO: add feature list and screenshots here

## Development

Development documentation can be found [here](./docs/dev/overview.md).

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
