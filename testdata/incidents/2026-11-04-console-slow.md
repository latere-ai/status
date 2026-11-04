---
title: Console is slow to load
services: [console, api]
started: 2026-11-04T05:55:00Z
resolved:
updates:
  - time: 2026-11-04T09:05:00Z
    status: Identified
    text: A database node is busy. We are moving load away from it.
  - time: 2026-11-04T06:05:00Z
    status: Investigating
    text: Pages in the console take several seconds to load.
---
The platform console loads slowly. Existing sessions and the API keep working.
