# Zoom SDK, Desktop Client, or Browser App Manipulation

Date: 2024-08-27

## Status

Accepted

## Context

We wanted to build a dockerized Zoom bot to join meetings and add features to meetings.
The target was an automated Zoom client that could connect to Zoom calls without registering as a Zoom app.

Options:

- Use the Zoom SDK and register the app with Zoom
- Automate the native Zoom desktop client
  - Connects as a regular Zoom client without app registration
  - Can be controlled with GUI automation tooling such as PyAutoGUI
- Automate the Zoom browser app
  - Connects via the Zoom web client without app registration
  - Can be controlled with browser automation tooling such as Selenium

We wanted to use the Zoom SDK to build the functionality.
However, use of the SDK required registering the app with Zoom.
All apps were blocked from use on the channels we wanted to use the bot on.
This prevented us from using the SDK to build the bot for our target use case.

Automating the native Zoom desktop client was conceptually aligned with the target,
but there was no ARM64 Zoom desktop client release available for our containerized environment.
This prevented us from using a native Zoom desktop client approach on our Mac development machines.

Automating the Zoom browser app remained viable on ARM64 and supported programmatic control.
This approach avoided Zoom app registration while still allowing meeting join automation.

Base image selection for this containerized browser automation approach is covered in
[ADR 0002](0002-Base-Image.md).

## Decision

We build our own solution by automating the Zoom browser app,
first creating a Docker container that can run on ARM64 processors,
then launching a browser in the Docker container to join meetings.

## Consequences

We lose out on the cleaner SDK-based integration model.
Extra work is needed to control the browser in the Docker container.
Feature parity with the native Zoom desktop client may also differ in some scenarios.

<!-- Footer links -->
