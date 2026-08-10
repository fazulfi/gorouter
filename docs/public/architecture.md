# Architecture

Gorouter separates the HTTP transport, provider routing, persistence, and frontend surfaces. Health and admin APIs are distinct; authentication protects administrative operations.

The frontend uses English and Indonesian catalogs with English as the fallback. Deployment-specific secrets remain outside the repository.