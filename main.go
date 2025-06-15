package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// RegisterConfig represents the configuration for a register
type RegisterConfig struct {
	Name    string `json:"name"`
	Format  string `json:"format"` // "decimal", "hex", "float", "boolean"
	Address uint16 `json:"address"`
}

// RegisterBlock represents a block of registers to read
type RegisterBlock struct {
	StartAddress uint16           `json:"startAddress"`
	Length       uint16           `json:"length"`
	Registers    []RegisterConfig `json:"registers"`
}

// ServerConfig represents the configuration for a Modbus server
type ServerConfig struct {
	ID             string          `json:"id"`
	Address        string          `json:"address"`
	Port           int             `json:"port"`
	PollRate       int             `json:"pollRate"`
	RegisterBlocks []RegisterBlock `json:"registerBlocks"`
}

// ConfigFile represents the entire configuration file
type ConfigFile struct {
	Servers []*ModbusServer `json:"servers"`
}

// ModbusDataModel represents the complete Modbus data model
type ModbusDataModel struct {
	DiscreteInputs   [10000]bool   // 1-bit read-only values (0-9999)
	Coils            [10000]bool   // 1-bit read-write values (10000-19999)
	InputRegisters   [10000]uint16 // 16-bit read-only values (30000-39999)
	HoldingRegisters [10000]uint16 // 16-bit read-write values (40000-49999)
}

// ModbusServer represents a single Modbus server configuration
type ModbusServer struct {
	ID             string                    `json:"id"`
	Address        string                    `json:"address"`
	Port           int                       `json:"port"`
	PollRate       int                       `json:"pollRate"`
	RegisterBlocks []RegisterBlock           `json:"registerBlocks"`
	client         *ModbusClient             `json:"-"`
	mu             sync.Mutex                `json:"-"`
	registerMap    map[uint16]RegisterConfig `json:"-"`
	dataModel      ModbusDataModel           `json:"-"`
	blocks         []RegisterBlock           `json:"-"`
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
								<th>Name</th>
								<th>Value</th>
								<th>Format</th>
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
			<td>{{.Name}}</td>
			<td class="register-value">{{.Value}}</td>
			<td>{{.Format}}</td>
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
	http.HandleFunc("/api/config/upload", handleConfigUpload)
	http.HandleFunc("/api/config", handleGetConfig)

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
			ID       string `json:"id" form:"id"`
			Address  string `json:"address" form:"address"`
			Port     int    `json:"port" form:"port"`
			PollRate int    `json:"pollRate" form:"pollRate"`
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

		}

		// Initialize the complete Modbus data model
		dataModel := ModbusDataModel{}

		server := &ModbusServer{
			ID:          config.ID,
			Address:     config.Address,
			Port:        config.Port,
			PollRate:    config.PollRate,
			registerMap: make(map[uint16]RegisterConfig),
			dataModel:   dataModel,
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
		data := make([]map[string]interface{}, 0)

		// Only include values that are configured in register blocks
		for _, block := range server.blocks {

			log.Printf("block: %+v", block)
			for i := uint16(0); i < block.Length; i++ {
				addr := block.StartAddress + i
				regConfig, hasConfig := server.registerMap[addr]
				if !hasConfig {
					// create default config
					regConfig = RegisterConfig{
						Name:    fmt.Sprintf("Register %d", addr),
						Format:  "decimal",
						Address: addr,
					}
				}

				// Get value from data model based on address range
				var value interface{}
				switch {
				case addr < 10000: // Coils
					if addr >= uint16(len(server.dataModel.Coils)) {
						// panic as this should not happen
						panic(fmt.Sprintf("Coil address out of range: %d", addr))
					}
					value = server.dataModel.Coils[addr]
				case addr < 20000: // Discrete Inputs
					if addr-10000 >= uint16(len(server.dataModel.DiscreteInputs)) {
						// panic as this should not happen
						panic(fmt.Sprintf("Discrete Input address out of range: %d", addr))
					}
					value = server.dataModel.DiscreteInputs[addr-10000]
				case addr < 40000: // Input Registers
					if addr-30000 >= uint16(len(server.dataModel.InputRegisters)) {
						// panic as this should not happen
						panic(fmt.Sprintf("Input Register address out of range: %d", addr))
					}
					value = server.dataModel.InputRegisters[addr-30000]
				default: // Holding Registers
					if addr-40000 >= uint16(len(server.dataModel.HoldingRegisters)) {
						// panic as this should not happen
						panic(fmt.Sprintf("Holding Register address out of range: %d", addr))
					}
					value = server.dataModel.HoldingRegisters[addr-40000]
				}

				// Format value based on format type
				var displayValue interface{}
				switch regConfig.Format {
				case "hex":
					if v, ok := value.(uint16); ok {
						displayValue = fmt.Sprintf("0x%04X", v)
					} else {
						displayValue = value
					}
				case "float":
					// get the next register and combine to form a float32
					if addr+1 >= block.StartAddress+block.Length {
						displayValue = "N/A"
					} else {
						if addr < 40000 { // input registers
							// combine to form a float32 by shifting the bytes
							bits := uint32(value.(uint16))<<16 + uint32(server.dataModel.InputRegisters[addr+1-30000])
							displayValue = math.Float32frombits(bits)
						} else {
							// combine to form a float32 by shifting the bytes
							bits := uint32(value.(uint16))<<16 + uint32(server.dataModel.HoldingRegisters[addr+1-40000])
							displayValue = math.Float32frombits(bits)
						}
					}
					i = i + 1
				case "boolean":
					if v, ok := value.(uint16); ok {
						displayValue = v != 0
					} else {
						displayValue = value
					}
				default: // decimal
					displayValue = value
				}

				data = append(data, map[string]interface{}{
					"Address": addr,
					"Name":    regConfig.Name,
					"Value":   displayValue,
					"Format":  regConfig.Format,
				})
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

// handleConfigUpload handles the upload of a configuration file or direct JSON configuration
func handleConfigUpload(w http.ResponseWriter, r *http.Request) {

	log.Printf("handleConfigUpload: %s %s", r.Method, r.URL.Path)
	// print headers
	for k, v := range r.Header {
		log.Printf("Header: %s: %s", k, v)
	}

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var config ConfigFile

	// Check if this is a file upload or direct JSON
	contentType := r.Header.Get("Content-Type")
	if strings.Contains(contentType, "multipart/form-data") {
		// Handle file upload
		if err := r.ParseMultipartForm(10 << 20); err != nil { // 10 MB max
			handleError(w, r, fmt.Sprintf("Failed to parse form: %v", err))
			return
		}

		file, _, err := r.FormFile("config")
		if err != nil {
			handleError(w, r, fmt.Sprintf("Failed to get file: %v", err))
			return
		}
		defer file.Close()

		// Read and parse JSON
		data, err := io.ReadAll(file)
		if err != nil {
			handleError(w, r, fmt.Sprintf("Failed to read file: %v", err))
			return
		}

		if err := json.Unmarshal(data, &config); err != nil {
			handleError(w, r, fmt.Sprintf("Invalid JSON: %v", err))
			log.Printf("Invalid JSON: %v", err)
			return
		}
	} else {
		// Handle direct JSON
		if err := json.NewDecoder(r.Body).Decode(&config); err != nil {
			handleError(w, r, fmt.Sprintf("Invalid JSON: %v", err))
			log.Printf("Invalid JSON: %v", err)
			return
		}
	}

	fmt.Printf("config: %+v", config)

	// Process each server in the config
	for _, server := range config.Servers {
		// Create register map
		registerMap := make(map[uint16]RegisterConfig)
		for _, block := range server.RegisterBlocks {
			for _, reg := range block.Registers {
				registerMap[reg.Address] = reg
			}
		}

		// Set additional fields
		server.client = nil // Will be set below
		server.registerMap = registerMap
		server.dataModel = ModbusDataModel{}
		server.blocks = server.RegisterBlocks

		// Create Modbus client
		client, err := NewModbusClient(server.Address, server.Port)
		if err != nil {
			handleError(w, r, fmt.Sprintf("Failed to create Modbus client for server %s: %v", server.ID, err))
			log.Printf("Failed to create Modbus client for server %s: %v", server.ID, err)
			continue
		}
		server.client = client

		// Add server to map
		mu.Lock()
		servers[server.ID] = server
		mu.Unlock()

		// Start polling in background
		go pollServer(server)
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
	})
}

// handleGetConfig returns the current server configuration
func handleGetConfig(w http.ResponseWriter, r *http.Request) {
	log.Printf("handleGetConfig: %s %s", r.Method, r.URL.Path)

	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	mu.RLock()
	defer mu.RUnlock()

	// Convert current servers to configuration format
	config := ConfigFile{
		Servers: make([]*ModbusServer, 0, len(servers)),
	}

	for _, server := range servers {
		server.mu.Lock()
		config.Servers = append(config.Servers, server)
		server.mu.Unlock()
	}

	err := json.NewEncoder(w).Encode(config)
	if err != nil {
		log.Printf("Error encoding config: %v", err)
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
	ticker := time.NewTicker(time.Duration(server.PollRate) * time.Millisecond)
	defer ticker.Stop()

	for range ticker.C {
		mu.RLock()
		_, exists := servers[server.ID]
		mu.RUnlock()

		if !exists {
			return
		}

		server.mu.Lock()
		// Process each register block
		for _, block := range server.blocks {
			switch {
			case block.StartAddress < 10000: // Coils
				values, err := server.client.ReadCoils(block.StartAddress, block.Length)
				if err != nil {
					log.Printf("Error reading coils for server %s at address %d: %v", server.ID, block.StartAddress, err)
					continue
				}
				log.Printf("Read Coils ok: startaddress: %+v, values: %+v", block.StartAddress, values)

				// update dataModel using values by copying values to dataModel
				copy(server.dataModel.Coils[block.StartAddress:block.StartAddress+block.Length], values)

			case block.StartAddress < 20000: // Discrete Inputs
				values, err := server.client.ReadDiscreteInputs(block.StartAddress-10000, block.Length)
				if err != nil {
					log.Printf("Error reading discrete inputs for server %s at address %d: %v", server.ID, block.StartAddress, err)
					continue
				}
				log.Printf("Read Holding Registers ok: startaddress: %+v, values: %+v", block.StartAddress, values)

				// update dataModel using values by copying values to dataModel
				copy(server.dataModel.DiscreteInputs[block.StartAddress-10000:block.StartAddress-10000+block.Length], values)

			case block.StartAddress < 40000: // Input Registers
				values, err := server.client.ReadInputRegisters(block.StartAddress-30000, block.Length)
				if err != nil {
					log.Printf("Error reading input registers for server %s at address %d: %v", server.ID, block.StartAddress, err)
					continue
				}
				log.Printf("Read Input Registers ok: startaddress: %+v, values: %+v", block.StartAddress, values)

				// update dataModel using values by copying values to dataModel
				copy(server.dataModel.InputRegisters[block.StartAddress-30000:block.StartAddress-30000+block.Length], values)

			default: // Holding Registers
				values, err := server.client.ReadHoldingRegisters(block.StartAddress-40000, block.Length)
				if err != nil {
					log.Printf("Error reading holding registers for server %s at address %d: %v", server.ID, block.StartAddress, err)
					continue
				}
				log.Printf("Read Holding Registers ok: startaddress: %+v, values: %+v", block.StartAddress, values)
				// update dataModel using values by copying values to dataModel
				copy(server.dataModel.HoldingRegisters[block.StartAddress-40000:block.StartAddress-40000+block.Length], values)
			}
		}
		server.mu.Unlock()
	}
}
