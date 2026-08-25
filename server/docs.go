package server

import (
	"net/http"

	"github.com/gin-gonic/gin"
	clientdocs "github.com/godopetza/pitchtz/docs"
)

func OpenAPISpec(c *gin.Context) {
	c.Data(http.StatusOK, "application/yaml; charset=utf-8", clientdocs.ClientOpenAPI)
}

func SwaggerUI(c *gin.Context) {
	c.Header("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline' https://cdn.jsdelivr.net; style-src 'self' 'unsafe-inline' https://cdn.jsdelivr.net; img-src 'self' data: https:; font-src 'self' https://cdn.jsdelivr.net; connect-src 'self' https:")
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width,initial-scale=1">
  <title>PitchTZ Mobile API</title>
  <link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5/swagger-ui.css">
  <style>body{margin:0;background:#f5f4ef}.topbar{display:none}.swagger-ui .info .title{color:#0e3b2c}.swagger-ui .btn.authorize{border-color:#0e3b2c;color:#0e3b2c}.swagger-ui .opblock-tag{color:#0e3b2c}</style>
</head>
<body>
  <div id="swagger-ui"></div>
  <script src="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
  <script>SwaggerUIBundle({url:'/openapi.yaml',dom_id:'#swagger-ui',deepLinking:true,displayRequestDuration:true,filter:true,persistAuthorization:true,tryItOutEnabled:true})</script>
</body>
</html>`))
}
