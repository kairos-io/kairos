package server

import (
	"github.com/kairos-io/AuroraBoot/docs"
)

// swaggerYAMLBytes is the OpenAPI document in YAML form. The generated
// docs package exposes it via SwaggerYAML (embedded from
// docs/swagger.yaml by docs/yaml_embed.go).
var swaggerYAMLBytes = docs.SwaggerYAML

// swaggerUIPage is a minimal, self-contained HTML page that boots
// Swagger UI from the jsDelivr CDN and points it at our served spec.
// Keeping this inline (rather than pulling in a swagger-ui Go package)
// avoids adding a large static-asset dependency for a developer-facing
// page. If the CDN is unreachable, the page still lists the spec URLs
// so users can consume it with their own tooling.
const swaggerUIPage = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8" />
  <meta name="viewport" content="width=device-width,initial-scale=1" />
  <title>AuroraBoot API — Swagger UI</title>
  <link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5/swagger-ui.css" />
  <style>
    body { margin: 0; background: #fafafa; }
    #swagger-ui { max-width: 1200px; margin: 0 auto; }
    .fallback {
      font-family: system-ui, -apple-system, sans-serif;
      max-width: 720px;
      margin: 4rem auto;
      padding: 1.5rem;
      border: 1px solid #e5e7eb;
      border-radius: 0.5rem;
      color: #374151;
    }
    .fallback code {
      background: #f3f4f6;
      padding: 0.15rem 0.4rem;
      border-radius: 0.25rem;
      font-size: 0.95em;
    }
  </style>
</head>
<body>
  <noscript>
    <div class="fallback">
      <h1>AuroraBoot API</h1>
      <p>Swagger UI needs JavaScript. The raw spec is still available at:</p>
      <ul>
        <li><a href="/api/v1/openapi.yaml"><code>GET /api/v1/openapi.yaml</code></a></li>
        <li><a href="/api/v1/openapi.json"><code>GET /api/v1/openapi.json</code></a></li>
      </ul>
    </div>
  </noscript>
  <div id="swagger-ui"></div>
  <script src="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5/swagger-ui-bundle.js" crossorigin></script>
  <script>
    window.addEventListener("load", function () {
      if (typeof SwaggerUIBundle === "undefined") {
        document.getElementById("swagger-ui").innerHTML =
          '<div class="fallback"><h1>AuroraBoot API</h1>' +
          '<p>Swagger UI failed to load from the CDN. The raw spec is at ' +
          '<a href="/api/v1/openapi.yaml">/api/v1/openapi.yaml</a> or ' +
          '<a href="/api/v1/openapi.json">/api/v1/openapi.json</a>.</p></div>';
        return;
      }
      SwaggerUIBundle({
        url: "/api/v1/openapi.json",
        dom_id: "#swagger-ui",
        deepLinking: true,
        presets: [
          SwaggerUIBundle.presets.apis,
          SwaggerUIBundle.SwaggerUIStandalonePreset
        ],
        layout: "BaseLayout",
      });
    });
  </script>
</body>
</html>`
