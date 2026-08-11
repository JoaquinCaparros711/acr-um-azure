package main

import (
	"encoding/json"
	"io"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

type ResponsePayload struct {
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

func Test_Should_ReturnSuccessMessage_When_GetRootEndpoint(t *testing.T) {
	// Arrange
	app := SetupApp()
	req := httptest.NewRequest("GET", "/", nil)

	// Act
	resp, err := app.Test(req, -1)

	// Assert
	assert.Nil(t, err)
	assert.Equal(t, 200, resp.StatusCode)

	bodyBytes, err := io.ReadAll(resp.Body)
	assert.Nil(t, err)

	var payload ResponsePayload
	err = json.Unmarshal(bodyBytes, &payload)
	assert.Nil(t, err)
	assert.Equal(t, "success", payload.Status)
	assert.Equal(t, "Hello World from Go and Fiber!", payload.Message)
}

func Test_Should_ReturnHealthyStatus_When_GetHealthEndpoint(t *testing.T) {
	// Arrange
	app := SetupApp()
	req := httptest.NewRequest("GET", "/health", nil)

	// Act
	resp, err := app.Test(req, -1)

	// Assert
	assert.Nil(t, err)
	assert.Equal(t, 200, resp.StatusCode)

	bodyBytes, err := io.ReadAll(resp.Body)
	assert.Nil(t, err)

	var payload ResponsePayload
	err = json.Unmarshal(bodyBytes, &payload)
	assert.Nil(t, err)
	assert.Equal(t, "healthy", payload.Status)
}
