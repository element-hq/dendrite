# Executive Summary

Dendrite (Go) is a capable Matrix homeserver for small-to-medium deployments, but it trails Synapse (Python) on Matrix 2.0 features and some operational tooling. Core messaging, federation, and most Client-Server API basics work; gaps remain around **Sliding Sync**, **OIDC/MAS**, **Threads (full)**, **3PID flows**, and richer **admin/moderation** APIs. This pack details parity gaps, concrete implementation plans, and a pragmatic roadmap—with more realistic effort estimates and explicit dependencies.
