package utils

import "github.com/gin-gonic/gin"

type ErrorPayload struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func RespondSuccess(c *gin.Context, status int, data interface{}, message string) {
	response := gin.H{"success": true, "data": data}
	if message != "" {
		response["message"] = message
	}
	c.JSON(status, response)
}

func RespondError(c *gin.Context, status int, code, message string) {
	c.JSON(status, gin.H{
		"success": false,
		"error":   ErrorPayload{Code: code, Message: message},
	})
}
