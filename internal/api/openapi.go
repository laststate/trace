package api

const openapiJSON = `{
  "openapi": "3.0.3",
  "info": {
    "title": "Last State Trace API",
    "version": "0.8.0",
    "description": "Observability for embedded firmware — ingest, issues, devices, releases, alerts. SAML/SCIM are experimental stubs."
  },
  "servers": [{"url": "/"}],
  "tags": [
    {"name": "health"}, {"name": "relay"}, {"name": "auth"}, {"name": "issues"},
    {"name": "events"}, {"name": "devices"}, {"name": "releases"}, {"name": "artifacts"},
    {"name": "orgs"}, {"name": "projects"}, {"name": "alerts"}, {"name": "admin"}
  ],
  "paths": {
    "/health/live": {"get": {"tags": ["health"], "summary": "Liveness", "responses": {"200": {"description": "ok"}}}},
    "/health/ready": {"get": {"tags": ["health"], "summary": "Readiness", "responses": {"200": {"description": "ok"}, "503": {"description": "not ready"}}}},
    "/metrics": {"get": {"tags": ["health"], "summary": "Prometheus metrics"}},
    "/openapi.json": {"get": {"tags": ["health"], "summary": "This OpenAPI document"}},
    "/v1/relay/capabilities": {"get": {"tags": ["relay"], "summary": "Relay capabilities"}},
    "/v1/ingest": {
      "post": {
        "tags": ["relay"], "summary": "Ingest LEP event",
        "security": [{"bearerAuth": []}],
        "parameters": [
          {"name": "Idempotency-Key", "in": "header", "schema": {"type": "string"}},
          {"name": "X-Last-State-Event-ID", "in": "header", "schema": {"type": "string"}}
        ],
        "requestBody": {"content": {"application/octet-stream": {"schema": {"type": "string", "format": "binary"}}}},
        "responses": {
          "202": {"description": "Accepted or idempotent duplicate (status=duplicate, duplicate=true)"},
          "422": {"description": "Conflict: same event_id, different payload hash (error.code=conflict)"},
          "401": {"description": "Unauthorized"},
          "403": {"description": "Missing scope"}
        }
      }
    },
    "/v1/events:batch": {"post": {"tags": ["relay"], "summary": "Batch ingest", "security": [{"bearerAuth": []}]}},
    "/v1/artifacts": {"post": {"tags": ["relay"], "summary": "Upload artifact", "security": [{"bearerAuth": []}]}},
    "/v1/relay/heartbeat": {"post": {"tags": ["relay"], "summary": "Relay heartbeat", "security": [{"bearerAuth": []}]}},
    "/api/auth/login": {
      "post": {
        "tags": ["auth"], "summary": "Password login",
        "requestBody": {"content": {"application/json": {"schema": {"type": "object", "properties": {"email": {"type": "string"}, "password": {"type": "string"}}}}}},
        "responses": {"200": {"description": "token + user"}, "401": {"description": "invalid"}}
      }
    },
    "/api/auth/register": {"post": {"tags": ["auth"], "summary": "Public register (disabled unless TRACE_ALLOW_PUBLIC_REGISTER)"}},
    "/api/auth/invite/accept": {"post": {"tags": ["auth"], "summary": "Accept org invite"}},
    "/api/auth/logout": {"post": {"tags": ["auth"], "summary": "Logout and clear session cookie"}},
    "/api/tokens": {"get": {"tags": ["admin"], "summary": "List tokens", "security": [{"bearerAuth": []}]}, "post": {"tags": ["admin"], "summary": "Create token", "security": [{"bearerAuth": []}]}},
    "/api/tokens/{id}": {"delete": {"tags": ["admin"], "summary": "Revoke token", "security": [{"bearerAuth": []}], "parameters": [{"name": "id", "in": "path", "required": true, "schema": {"type": "string", "format": "uuid"}}]}},
    "/api/auth/oidc/login": {"get": {"tags": ["auth"], "summary": "OIDC authorize redirect"}},
    "/api/auth/oidc/callback": {"get": {"tags": ["auth"], "summary": "OIDC callback (requires existing membership)"}},
    "/api/me": {"get": {"tags": ["auth"], "summary": "Current user", "security": [{"bearerAuth": []}]}},
    "/api/overview": {"get": {"tags": ["issues"], "summary": "Project overview", "security": [{"bearerAuth": []}]}},
    "/api/query/events": {"get": {"tags": ["events"], "summary": "Event query DSL", "security": [{"bearerAuth": []}]}},
    "/api/oncall": {"get": {"tags": ["admin"], "summary": "On-call schedules"}, "post": {"tags": ["admin"], "summary": "Create schedule"}},
    "/api/escalation": {"get": {"tags": ["admin"], "summary": "Escalation policies"}, "post": {"tags": ["admin"], "summary": "Create policy"}},
    "/api/search": {
      "get": {
        "tags": ["admin"], "summary": "Global search",
        "security": [{"bearerAuth": []}],
        "parameters": [{"name": "q", "in": "query", "required": true, "schema": {"type": "string"}}]
      }
    },
    "/api/issues": {
      "get": {
        "tags": ["issues"], "summary": "List issues",
        "security": [{"bearerAuth": []}],
        "parameters": [
          {"name": "limit", "in": "query", "schema": {"type": "integer"}},
          {"name": "offset", "in": "query", "schema": {"type": "integer"}},
          {"name": "status", "in": "query", "schema": {"type": "string"}},
          {"name": "severity", "in": "query", "schema": {"type": "string"}},
          {"name": "q", "in": "query", "schema": {"type": "string"}},
          {"name": "X-Project-ID", "in": "header", "schema": {"type": "string", "format": "uuid"}}
        ]
      }
    },
    "/api/issues/{id}": {"get": {"tags": ["issues"], "summary": "Issue detail", "security": [{"bearerAuth": []}], "parameters": [{"name": "id", "in": "path", "required": true, "schema": {"type": "string", "format": "uuid"}}]}},
    "/api/issues/{id}/status": {"post": {"tags": ["issues"], "summary": "Update status", "security": [{"bearerAuth": []}]}},
    "/api/issues/{id}/comments": {"post": {"tags": ["issues"], "summary": "Add comment", "security": [{"bearerAuth": []}]}},
    "/api/issues/{id}/suspect-commits": {"get": {"tags": ["issues"], "summary": "Suspect commits"}},
    "/api/issues/{id}/replay": {"get": {"tags": ["issues"], "summary": "Breadcrumb replay"}},
    "/api/issues/{id}/assign": {"post": {"tags": ["issues"], "summary": "Assign user", "security": [{"bearerAuth": []}]}},
    "/api/issues/{id}/merge": {"post": {"tags": ["issues"], "summary": "Merge into target", "security": [{"bearerAuth": []}]}},
    "/api/issues/{id}/split": {"post": {"tags": ["issues"], "summary": "Split events to new issue", "security": [{"bearerAuth": []}]}},
    "/api/issues/{id}/labels": {"post": {"tags": ["issues"], "summary": "Set labels", "security": [{"bearerAuth": []}]}},
    "/api/events": {"get": {"tags": ["events"], "summary": "List events", "security": [{"bearerAuth": []}]}},
    "/api/events/{id}": {"get": {"tags": ["events"], "summary": "Event detail", "security": [{"bearerAuth": []}]}},
    "/api/events/{id}/raw": {"get": {"tags": ["events"], "summary": "Download raw LEP", "security": [{"bearerAuth": []}]}},
    "/api/events/{id}/reprocess": {"post": {"tags": ["events"], "summary": "Reprocess event", "security": [{"bearerAuth": []}]}},
    "/api/events/reprocess-stale": {"post": {"tags": ["events"], "summary": "Reprocess stale analyzer versions", "security": [{"bearerAuth": []}]}},
    "/api/devices": {"get": {"tags": ["devices"], "summary": "List devices", "security": [{"bearerAuth": []}]}},
    "/api/devices/{id}": {"get": {"tags": ["devices"], "summary": "Device detail", "security": [{"bearerAuth": []}]}},
    "/api/devices/{id}/firmware-history": {"get": {"tags": ["devices"], "summary": "Firmware history", "security": [{"bearerAuth": []}]}},
    "/api/releases": {"get": {"tags": ["releases"], "summary": "List releases", "security": [{"bearerAuth": []}]}},
    "/api/releases/{id}/stats": {"get": {"tags": ["releases"], "summary": "Crash-free rate", "security": [{"bearerAuth": []}]}},
    "/api/artifacts": {"get": {"tags": ["artifacts"], "summary": "List artifacts", "security": [{"bearerAuth": []}]}},
    "/api/organizations": {"get": {"tags": ["orgs"], "summary": "List orgs"}, "post": {"tags": ["orgs"], "summary": "Create org"}, "patch": {"tags": ["orgs"], "summary": "Update current org"}},
    "/api/organizations/members": {"get": {"tags": ["orgs"], "summary": "List members"}, "post": {"tags": ["orgs"], "summary": "Invite"}, "patch": {"tags": ["orgs"], "summary": "Update role"}, "delete": {"tags": ["orgs"], "summary": "Remove member"}},
    "/api/projects": {"get": {"tags": ["projects"], "summary": "List projects"}, "post": {"tags": ["projects"], "summary": "Create project"}},
    "/api/projects/{id}": {"patch": {"tags": ["projects"], "summary": "Update"}, "delete": {"tags": ["projects"], "summary": "Delete"}},
    "/api/alerts": {"get": {"tags": ["alerts"], "summary": "List alert rules"}, "post": {"tags": ["alerts"], "summary": "Create alert rule"}},
    "/api/channels": {"get": {"tags": ["alerts"], "summary": "List notification channels"}, "post": {"tags": ["alerts"], "summary": "Create channel (slack|discord|email|github|webhook)"}},
    "/api/relays": {"get": {"tags": ["relay"], "summary": "List relays"}},
    "/api/hardware": {"get": {"tags": ["devices"], "summary": "Hardware revisions"}, "post": {"tags": ["devices"], "summary": "Create revision"}},
    "/api/hardware/compare": {"get": {"tags": ["devices"], "summary": "Failures by hardware revision"}},
    "/api/tokens": {"post": {"tags": ["admin"], "summary": "Create project token"}},
    "/api/audit": {"get": {"tags": ["admin"], "summary": "Audit log"}}
  },
  "components": {
    "securitySchemes": {
      "bearerAuth": {"type": "http", "scheme": "bearer", "bearerFormat": "token"}
    },
    "parameters": {
      "ProjectHeader": {
        "name": "X-Project-ID", "in": "header", "schema": {"type": "string", "format": "uuid"},
        "description": "Target project (must belong to user organization)"
      }
    }
  },
  "security": [{"bearerAuth": []}]
}`
