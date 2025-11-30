package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

// MCPRequest represents a JSON-RPC 2.0 request
type MCPRequest struct {
	JSONRPC string                 `json:"jsonrpc"`
	ID      interface{}            `json:"id"`
	Method  string                 `json:"method"`
	Params  map[string]interface{} `json:"params,omitempty"`
}

// MCPResponse represents a JSON-RPC 2.0 response
type MCPResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id"`
	Result  interface{} `json:"result,omitempty"`
	Error   *MCPError   `json:"error,omitempty"`
}

// MCPError represents a JSON-RPC 2.0 error
type MCPError struct {
	Code    int                    `json:"code"`
	Message string                 `json:"message"`
	Data    map[string]interface{} `json:"data,omitempty"`
}

// Mock weather data
var weatherData = map[string]map[string]interface{}{
	"san francisco": {
		"temperature": 18.5,
		"humidity":    65,
		"condition":   "Partly Cloudy",
		"wind_speed":  12.3,
	},
	"new york": {
		"temperature": 10.2,
		"humidity":    55,
		"condition":   "Clear",
		"wind_speed":  8.5,
	},
	"london": {
		"temperature": 12.0,
		"humidity":    80,
		"condition":   "Rainy",
		"wind_speed":  15.0,
	},
	"tokyo": {
		"temperature": 22.0,
		"humidity":    70,
		"condition":   "Sunny",
		"wind_speed":  5.0,
	},
}

func main() {
	// Handle both root and /mcp for compatibility
	http.HandleFunc("/", handleMCP)
	http.HandleFunc("/mcp", handleMCP)
	http.HandleFunc("/health", handleHealth)

	port := ":8082"
	log.Printf("🌤️  Mock Weather MCP Server starting on http://localhost%s\n", port)
	log.Printf("📡 MCP endpoint: http://localhost%s/mcp\n", port)
	log.Printf("💚 Health endpoint: http://localhost%s/health\n", port)
	log.Println("✨ Ready to handle weather requests!")

	if err := http.ListenAndServe(port, nil); err != nil {
		log.Fatal(err)
	}
}

func handleMCP(w http.ResponseWriter, r *http.Request) {
	// CORS headers
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Read request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		sendError(w, nil, -32700, "Parse error")
		return
	}
	defer r.Body.Close()

	// Parse JSON-RPC request
	var req MCPRequest
	if err := json.Unmarshal(body, &req); err != nil {
		sendError(w, nil, -32700, "Parse error")
		return
	}

	log.Printf("📥 Received: %s (id=%v)\n", req.Method, req.ID)

	// Handle methods
	var resp *MCPResponse
	switch req.Method {
	case "initialize":
		resp = handleInitialize(&req)
	case "list_tools":
		resp = handleListTools(&req)
	case "call_tool":
		resp = handleCallTool(&req)
	default:
		resp = &MCPResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &MCPError{
				Code:    -32601,
				Message: fmt.Sprintf("Method not found: %s", req.Method),
			},
		}
	}

	// Send response
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func handleInitialize(req *MCPRequest) *MCPResponse {
	log.Println("✅ Initializing MCP connection")

	return &MCPResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]interface{}{
			"protocolVersion": "1.0",
			"capabilities":    []string{"tools"},
			"serverInfo": map[string]string{
				"name":    "Mock Weather Server",
				"version": "1.0.0",
			},
		},
	}
}

func handleListTools(req *MCPRequest) *MCPResponse {
	log.Println("📋 Listing available tools")

	tools := []map[string]interface{}{
		{
			"name":        "get_current",
			"description": "Get current weather for a city",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"city": map[string]interface{}{
						"type":        "string",
						"description": "City name",
					},
				},
				"required": []string{"city"},
			},
		},
		{
			"name":        "add",
			"description": "Add two numbers together",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"a": map[string]interface{}{
						"type":        "number",
						"description": "First number",
					},
					"b": map[string]interface{}{
						"type":        "number",
						"description": "Second number",
					},
				},
				"required": []string{"a", "b"},
			},
		},
	}

	return &MCPResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]interface{}{
			"tools": tools,
		},
	}
}

func handleCallTool(req *MCPRequest) *MCPResponse {
	// Extract tool name
	toolName, ok := req.Params["name"].(string)
	if !ok {
		return &MCPResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &MCPError{
				Code:    -32602,
				Message: "Missing required parameter: name",
			},
		}
	}

	// Extract arguments
	args, ok := req.Params["arguments"].(map[string]interface{})
	if !ok {
		args = make(map[string]interface{})
	}

	log.Printf("🔧 Calling tool: %s with args: %v\n", toolName, args)

	// Handle different tools
	switch toolName {
	case "get_current":
		return handleWeatherTool(req, args)
	case "add":
		return handleAddTool(req, args)
	default:
		return &MCPResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &MCPError{
				Code:    -32602,
				Message: fmt.Sprintf("Unknown tool: %s", toolName),
			},
		}
	}
}

func handleWeatherTool(req *MCPRequest, args map[string]interface{}) *MCPResponse {
	// Get city from arguments
	city, ok := args["city"].(string)
	if !ok || city == "" {
		return &MCPResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &MCPError{
				Code:    -32602,
				Message: "Missing required argument: city",
			},
		}
	}

	// Normalize city name
	cityLower := normalizeCity(city)

	// Get weather data
	weather, ok := weatherData[cityLower]
	if !ok {
		// Default weather for unknown cities
		weather = map[string]interface{}{
			"temperature": 20.0,
			"humidity":    60,
			"condition":   "Unknown",
			"wind_speed":  10.0,
			"note":        fmt.Sprintf("Weather data not available for %s, showing default", city),
		}
	}

	// Add timestamp
	weather["timestamp"] = time.Now().Format(time.RFC3339)
	weather["city"] = city

	log.Printf("🌤️  Weather for %s: %.1f°C, %s\n", city, weather["temperature"], weather["condition"])

	return &MCPResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]interface{}{
			"content": []map[string]interface{}{
				{
					"type": "text",
					"text": formatWeather(city, weather),
				},
			},
			"data": weather,
		},
	}
}

func handleAddTool(req *MCPRequest, args map[string]interface{}) *MCPResponse {
	// Get numbers from arguments
	a, aOk := args["a"].(float64)
	if !aOk {
		// Try to convert from int
		if aInt, ok := args["a"].(int); ok {
			a = float64(aInt)
			aOk = true
		}
	}

	b, bOk := args["b"].(float64)
	if !bOk {
		// Try to convert from int
		if bInt, ok := args["b"].(int); ok {
			b = float64(bInt)
			bOk = true
		}
	}

	if !aOk || !bOk {
		return &MCPResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &MCPError{
				Code:    -32602,
				Message: "Missing required arguments: a and b must be numbers",
			},
		}
	}

	result := a + b
	log.Printf("🔢 Math: %.2f + %.2f = %.2f\n", a, b, result)

	return &MCPResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]interface{}{
			"content": []map[string]interface{}{
				{
					"type": "text",
					"text": fmt.Sprintf("%.2f + %.2f = %.2f", a, b, result),
				},
			},
			"result": result,
		},
	}
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "healthy",
		"service": "Mock Weather MCP Server",
		"version": "1.0.0",
		"time":    time.Now().Format(time.RFC3339),
	})
}

func sendError(w http.ResponseWriter, id interface{}, code int, message string) {
	resp := &MCPResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error: &MCPError{
			Code:    code,
			Message: message,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK) // JSON-RPC errors still return 200
	json.NewEncoder(w).Encode(resp)
}

func normalizeCity(city string) string {
	// Simple normalization: lowercase and trim
	normalized := ""
	for _, char := range city {
		if char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char == ' ' {
			if char >= 'A' && char <= 'Z' {
				normalized += string(char + 32)
			} else {
				normalized += string(char)
			}
		}
	}
	return normalized
}

func formatWeather(city string, weather map[string]interface{}) string {
	temp := weather["temperature"]
	condition := weather["condition"]
	humidity := weather["humidity"]
	windSpeed := weather["wind_speed"]

	return fmt.Sprintf(
		"Weather in %s:\n- Temperature: %.1f°C\n- Condition: %s\n- Humidity: %.0f%%\n- Wind Speed: %.1f km/h",
		city, temp, condition, humidity, windSpeed,
	)
}
