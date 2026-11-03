# Sehat

Sehat is a direct-to-consumer personal health records platform. People upload their reports, review the extracted values, track biomarkers over time, and ask questions answered from their own approved records and vetted research, with citations and safety checks.

> Prototype: not a medical device. It does not diagnose or prescribe.

## Start here

- [Getting Started](Getting-Started): run the stack locally
- [Architecture](Architecture): components and data flow
- [Modules and Status](Modules-and-Status): what each module does and how complete it is
- [Authentication](Authentication): Google and Apple sign-in, sessions and devices
- [API Overview](API-Overview): endpoint groups
- [Web App](Web-App): the React app and its UI test suite
- [Security and Privacy](Security-and-Privacy)
- [Testing and CI](Testing-and-CI): the single `ci.yml` pipeline
- [Configuration](Configuration): environment variables
- [Roadmap](Roadmap)

## Principles

- The Go API is the only authorization and data boundary. The Python AI service never touches the health database.
- Extracted values are candidates until a person approves them.
- No clinical data is hard-coded; reference knowledge is learned, reviewed and approved.
- Web and iOS behave the same.
