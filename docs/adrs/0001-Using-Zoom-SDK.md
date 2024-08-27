# Use of the zoom SDK

Date: 2024-08-27

## Status

Accepted

## Context

We wanted to build a dockerized zoom bot to join meetings and add features to the meeting.

Options:
- Use the Zoom SDK and register the app with zoom
- Extend [kastldratza/zoomrec][kastldratza]
    - Starts and launches zoom client in a docker container
    - Controls app with pyautogui
- Extend [mdouchement/docker-zoom-us][mdouchement]
    - Starts and launches Zoom using Iceweasel (Firefox) in a docker container
    - No automated control of the app
- Build our own solution

We wanted to use the zoom SDK to build the functionality.
However, at this time use of the SDK requires registering the app with zoom.
All apps are blocked from use on the channels we wanted to use the bot on.
This prevents us from using the SDK to build the bot.

[ZoomRec][kastldratza] looked like a good option, but it is behind in zoom versions,
and the docker instance can not run on arm64 processors.
Attempts to update the docker instance on an arm64 processor failed,
as there is no release of the zoom client for arm64 processors.
This prevents us from using ZoomRec to build the bot on our mac machines.

[Docker-Zoom-us][mdouchement] launches a browser in the docker container to join the meeting.
This should be able to run on arm64 processors.
The existing solution though is not built with arm64 processors in mind.
It also lacks the ability to control the zoom client pragmatically.

Building our own solution would require us to build a docker container that can run on arm64 processors.
We still will not be able to use the SDK, nor install the native zoom client on the arm64 processor.
however launching a browser in the docker container should work, 
and we can control the browser with pyautogui, selenium, or other browser automation tools.

## Decision

We will use build our own solution, 
first creating a docker container that can run on arm64 processors.
Then launching a browser in the docker container to join the meeting.

## Consequences

We will loose out on the clean solution of the SDK.
Extra work will be needed to control the browser in the docker container.

<!-- Footer links -->
[kastldratza]: https://github.com/kastldratza/zoomrec
[mdouchement]: https://github.com/mdouchement/docker-zoom-us