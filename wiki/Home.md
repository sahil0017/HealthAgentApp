# Sehat

Sehat is a direct-to-consumer personal health records platform. People upload their reports, review the extracted values, track biomarkers over time, and ask questions answered from their own approved records and vetted research, with citations and safety checks.

> Prototype: not a production medical product. It does not diagnose or prescribe; clinical and regulatory assessment is needed for intended uses.

## Start here

- [Getting Started](Getting-Started): run the stack locally
- [Architecture](Architecture): components and data flow
- [Modules and Status](Modules-and-Status): what each module does and how complete it is
- [Authentication](Authentication): Google and Apple sign-in, sessions and devices
- [API Overview](API-Overview): endpoint groups
- [Security and Privacy](Security-and-Privacy)
- [Testing and CI](Testing-and-CI): the single `ci.yml` pipeline
- [Configuration](Configuration): environment variables
- [Roadmap](Roadmap)

## Principles

- The Go API is the only authorization and data boundary. The Python AI service never touches the health database.
- Extracted values are candidates until a person approves them.
- Clinical reference content and generated explanations require provenance and review; the presence of a feature is not clinical validation.
- One Flutter app serves iPhone and Android.
