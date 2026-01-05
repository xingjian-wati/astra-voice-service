package gemini

import (
	"encoding/json"
	"fmt"

	"github.com/ClareAI/astra-voice-service/pkg/data/mcp"
	"github.com/ClareAI/astra-voice-service/pkg/logger"
	"go.uber.org/zap"
)

// executeFunctionCall executes a function call and returns the result to Gemini.
func (h *Handler) executeFunctionCall(connectionID string, toolCall map[string]interface{}) {
	name, _ := toolCall["name"].(string)
	id, _ := toolCall["id"].(string)
	argsStr, _ := toolCall["args"].(string)

	var args map[string]interface{}
	json.Unmarshal([]byte(argsStr), &args)

	// Trace active function calls
	logger.Base().Info("Executing tool call", zap.String("connection_id", connectionID), zap.String("name", name), zap.String("id", id))

	// Determine modality (for MCP tools)
	modality := mcp.ModalityVoiceInbound
	if h.ConnectionGetter != nil {
		if conn := h.ConnectionGetter(connectionID); conn != nil {
			if conn.GetIsOutbound() {
				modality = mcp.ModalityVoiceOutbound
			}
		}
	}

	// Execute function
	if h.ToolManager == nil {
		logger.Base().Error("ToolManager not initialized")
		h.sendFunctionResult(connectionID, id, name, map[string]interface{}{"error": "Tool manager not initialized"})
		return
	}

	result, err := h.ToolManager.ExecuteTool(name, argsStr, connectionID, modality)

	// Log action
	if err != nil {
		logger.Base().Error("Tool execution failed", zap.Error(err))
		h.sendFunctionResult(connectionID, id, name, map[string]interface{}{"error": err.Error()})
		return
	}

	// Send result back to Gemini
	h.sendFunctionResult(connectionID, id, name, result)
}

// executeFunction is a unified entry point for function execution.
func (h *Handler) executeFunction(connectionID string, name string, args map[string]interface{}) (interface{}, error) {
	if h.ToolManager == nil {
		return nil, fmt.Errorf("tool manager not initialized")
	}

	argsBytes, _ := json.Marshal(args)
	modality := mcp.ModalityVoiceInbound
	if h.ConnectionGetter != nil {
		if conn := h.ConnectionGetter(connectionID); conn != nil {
			if conn.GetIsOutbound() {
				modality = mcp.ModalityVoiceOutbound
			}
		}
	}

	return h.ToolManager.ExecuteTool(name, string(argsBytes), connectionID, modality)
}

// sendFunctionResult sends the function execution result back in Gemini's toolResponse format.
func (h *Handler) sendFunctionResult(connectionID string, id string, name string, result interface{}) {
	// Gemini requires response to be an object, not a JSON string.
	responseObj := result
	if str, ok := result.(string); ok {
		// If it's not JSON, wrap it as an object.
		var mapResult map[string]interface{}
		if err := json.Unmarshal([]byte(str), &mapResult); err == nil {
			responseObj = mapResult
		} else {
			responseObj = map[string]interface{}{"result": str}
		}
	}

	event := map[string]interface{}{
		"toolResponse": map[string]interface{}{
			"functionResponses": []map[string]interface{}{
				{
					"id":       id,
					"name":     name,
					"response": responseObj,
				},
			},
		},
	}

	h.sendEvent(connectionID, event)
}
