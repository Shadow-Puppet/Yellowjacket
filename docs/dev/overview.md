# Development Overview

YellowJacket is a moderately complex application. This document gives an overview of how development of it works.

## Logical Breakdown

YellowJacket can be thought about in a heirarchy of logical modules and components. The borders of these logical sections are mostly represented in the code and directory structure as well.

- Frontend
  - UI Components (see [Lit](###lit-web-components))
- Backend
  - App
    - Asset Handler
    - Logging
    - System
  - Player
  - Library
  - Config
  - Database
    - Queries (see [sqlc](###sqlc))

## Dependencies

YellowJacket uses many tools and libraries to provide its functionality. 
This section lists each of these dependencies and explains how they are used.

### [Wails](https://wails.io)

Used to create desktop apps with Go and web technologies.

### [SQLite](https://github.com/mattn/go-sqlite3?tab=readme-ov-file#go-sqlite3)

Used for local database.

### [sqlc](https://sqlc.dev/)

Used to generate Go code from SQL.

### [Templ](https://templ.guide/)

Used to generate HTML templates with Go code.

### [Beep](https://github.com/TheCodeOfCaleb/beep/v2?tab=readme-ov-file#beep)

Used for audio playback.

### [Lit Web Components](https://lit.dev/)

Used for dynamic/reactive frontend components.

### [HTMX](https://htmx.org/)

Used for requesting HTML fragments from the backend and rendering them on the frontend.
