package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ModbusServer represents a single Modbus server configuration
type ModbusServer struct {
	ID             string
	Address        string
	Port           int
	PollRate       time.Duration
	StartAddress   uint16
	RegisterLength uint16
	client         *ModbusClient
	mu             sync.Mutex
	values         []interface{}
}

var (
	servers = make(map[string]*ModbusServer)
	mu      sync.RWMutex
)

// HTML templates
const (
	serverListTemplate = `
		{{range .}}
		<div class="card mb-4" id="server-{{.ID}}">
			<div class="card-header d-flex justify-content-between align-items-center">
				<h5 class="mb-0">Server: {{.ID}}</h5>
				<button class="btn btn-danger btn-sm" 
						hx-delete="/api/servers/{{.ID}}"
						hx-confirm="Are you sure you want to remove server {{.ID}}?"
						hx-target="#server-{{.ID}}"
						hx-swap="outerHTML swap:1s">Remove</button>
			</div>
			<div class="card-body">
				<div class="table-responsive">
					<table class="table table-striped table-hover">
						<thead>
							<tr>
								<th>Address</th>
								<th>Decimal</th>
								<th>Hex</th>
							</tr>
						</thead>
						<tbody hx-get="/api/servers/{{.ID}}" 
							   hx-trigger="load, every 1s" 
							   hx-swap="innerHTML">
						</tbody>
					</table>
				</div>
			</div>
		</div>
		{{end}}`

	registerTableTemplate = `
		{{range .}}
		<tr>
			<td>{{.Address}}</td>
			<td class="register-value">{{.Value}}</td>
			<td class="register-value">{{.Hex}}</td>
		</tr>
		{{end}}`
)

func main() {
	// Serve static files
	fs := http.FileServer(http.Dir("static"))
	http.Handle("/static/", http.StripPrefix("/static/", fs))
	http.Handle("/", fs)

	// API endpoints
	http.HandleFunc("/api/servers", handleServers)
	http.HandleFunc("/api/servers/", handleServer)

	// Start the server
	port := 8080
	log.Printf("Starting server on port %d...", port)
	if err := http.ListenAndServe(fmt.Sprintf(":%d", port), nil); err != nil {
		log.Fatal(err)
	}
}

func handleServers(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		// Return list of servers
		mu.RLock()
		serverList := make([]map[string]string, 0, len(servers))
		for id := range servers {
			serverList = append(serverList, map[string]string{"ID": id})
		}
		mu.RUnlock()

		if isHtmxRequest(r) {
			tmpl := template.Must(template.New("serverList").Parse(serverListTemplate))
			w.Header().Set("Content-Type", "text/html")
			tmpl.Execute(w, serverList)
		} else {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"servers": serverList,
			})
		}

	case http.MethodPost:
		// Add new server
		var config struct {
			ID             string `json:"id" form:"id"`
			Address        string `json:"address" form:"address"`
			Port           int    `json:"port" form:"port"`
			PollRate       int    `json:"pollRate" form:"pollRate"`
			StartAddress   uint16 `json:"startAddress" form:"startAddress"`
			RegisterLength uint16 `json:"registerLength" form:"registerLength"`
		}

		// Handle both JSON and form data
		if r.Header.Get("Content-Type") == "application/json" {
			if err := json.NewDecoder(r.Body).Decode(&config); err != nil {
				handleError(w, r, fmt.Sprintf("Invalid request body: %v", err))
				return
			}
		} else {
			if err := r.ParseForm(); err != nil {
				handleError(w, r, fmt.Sprintf("Invalid form data: %v", err))
				return
			}
			config.ID = r.FormValue("id")
			config.Address = r.FormValue("address")
			config.Port, _ = strconv.Atoi(r.FormValue("port"))
			config.PollRate, _ = strconv.Atoi(r.FormValue("pollRate"))
			startAddr, _ := strconv.ParseUint(r.FormValue("startAddress"), 10, 16)
			config.StartAddress = uint16(startAddr)
			regLen, _ := strconv.ParseUint(r.FormValue("registerLength"), 10, 16)
			config.RegisterLength = uint16(regLen)
		}

		server := &ModbusServer{
			ID:             config.ID,
			Address:        config.Address,
			Port:           config.Port,
			PollRate:       time.Duration(config.PollRate) * time.Millisecond,
			StartAddress:   config.StartAddress,
			RegisterLength: config.RegisterLength,
			values:         make([]interface{}, config.RegisterLength),
		}

		// Create Modbus client
		client, err := NewModbusClient(server.Address, server.Port)
		if err != nil {
			handleError(w, r, fmt.Sprintf("Failed to create Modbus client: %v", err))
			return
		}
		server.client = client

		// Add server to map
		mu.Lock()
		servers[server.ID] = server
		mu.Unlock()

		// Start polling in background
		go pollServer(server)

		w.WriteHeader(http.StatusCreated)
		if isHtmxRequest(r) {
			w.Header().Set("HX-Trigger", "load")
		} else {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": true,
			})
		}

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func handleServer(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Path[len("/api/servers/"):]
	if id == "" {
		http.Error(w, "Server ID required", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodGet:
		mu.RLock()
		server, exists := servers[id]
		mu.RUnlock()

		if !exists {
			handleError(w, r, fmt.Sprintf("Server not found: %s", id))
			return
		}

		server.mu.Lock()
		defer server.mu.Unlock()

		// Convert register values to JSON-serializable format
		data := make([]map[string]interface{}, server.RegisterLength)
		for i := uint16(0); i < server.RegisterLength; i++ {
			value := server.values[i]
			if value == nil {
				value = "N/A"
			}
			data[i] = map[string]interface{}{
				"Address": server.StartAddress + i,
				"Value":   value,
				"Hex":     fmt.Sprintf("0x%04X", value),
			}
		}

		if isHtmxRequest(r) {
			tmpl := template.Must(template.New("registerTable").Parse(registerTableTemplate))
			w.Header().Set("Content-Type", "text/html")
			tmpl.Execute(w, data)
		} else {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": true,
				"data":    data,
			})
		}

	case http.MethodDelete:
		mu.Lock()
		server, exists := servers[id]
		if exists {
			if server.client != nil {
				server.client.Close()
			}
			delete(servers, id)
		}
		mu.Unlock()

		if !exists {
			handleError(w, r, fmt.Sprintf("Server not found: %s", id))
			return
		}

		if isHtmxRequest(r) {
			w.WriteHeader(http.StatusOK)
		} else {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": true,
			})
		}

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// Helper functions
func isHtmxRequest(r *http.Request) bool {
	return strings.Contains(r.Header.Get("HX-Request"), "true")
}

func handleError(w http.ResponseWriter, r *http.Request, message string) {
	log.Print(message)
	if isHtmxRequest(r) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, `<div class="alert alert-danger">%s</div>`, message)
	} else {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   message,
		})
	}
}

// pollServer continuously polls a Modbus server for data
func pollServer(server *ModbusServer) {
	ticker := time.NewTicker(server.PollRate)
	defer ticker.Stop()

	for range ticker.C {
		mu.RLock()
		_, exists := servers[server.ID]
		mu.RUnlock()

		if !exists {
			return
		}

		server.mu.Lock()
		values, err := server.client.ReadHoldingRegisters(server.StartAddress, server.RegisterLength)
		if err != nil {
			log.Printf("Error reading registers for server %s: %v", server.ID, err)
			server.mu.Unlock()
			continue
		}
		server.values = values
		server.mu.Unlock()
	}
}
