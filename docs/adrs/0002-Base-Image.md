# Base image selection

Date: 2024-09-16

## Status

Pending

## Context

We needed to select a base image for our Docker containers.
We were building an app that simulates a user on a desktop app, which is not a core feature of Docker.
This work followed the decision in ADR 0001 to build our own browser-based Zoom automation solution.

Constraints:

- Must run on ARM64 processors (including Mac development machines)
- Must support browser automation tooling
- Must provide remote GUI access for debugging and manual operation

Options:

- [linuxserver/firefox][firefox]
  - Already includes Firefox and KasmVNC for remote operation
  - Uses Alpine distro
- [linuxserver/baseimage-kasmvnc][kasmvnc] base image
  - Includes KasmVNC for remote operation
  - Provides Alpine, Arch, Debian, Fedora, and Ubuntu distros
- Extend [kastldratza/zoomrec][kastldratza]
  - Starts and launches Zoom client in a Docker container
  - Controls app with PyAutoGUI
  - Behind in Zoom versions
  - Does not work on ARM64 because there is no ARM64 Zoom client release
- Extend [mdouchement/docker-zoom-us][mdouchement]
  - Starts and launches Zoom client in a Docker container
  - Exposes a VNC server to control the app
  - Lacks automated control of the app
  - Does not work on ARM64 because there is no ARM64 Zoom client release

We attempted to install automation tooling on the Firefox image, but the Alpine distro was too restrictive.
Zoom client-based approaches were eliminated because they are not viable on ARM64 processors.

## Decision

We use the [linuxserver/baseimage-kasmvnc][kasmvnc] base image, and the Ubuntu distro release.
The use of Alpine for the distro for the Firefox image provides too many restrictions for the supporting software.

## Consequences

We lose the lightweight nature of the Alpine distro, but gain the flexibility of the Ubuntu distro.

<!-- Footer links -->

[firefox]: https://docs.linuxserver.io/images/docker-firefox/
[kasmvnc]: https://github.com/linuxserver/docker-baseimage-kasmvnc/pkgs/container/baseimage-kasmvnc
[kastldratza]: https://github.com/kastldratza/zoomrec
[mdouchement]: https://github.com/mdouchement/docker-zoom-us
