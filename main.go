package main

import (
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"io"
	"log"
	"math"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

//go:embed static
var staticFiles embed.FS

// LogLevel represents the logging level
type LogLevel int

const (
	ErrorLevel LogLevel = iota
	InfoLevel
	DebugLevel
)

var (
	servers   = make(map[string]*ModbusServer)
	mu        sync.RWMutex
	logLevel  LogLevel
	templates *template.Template
)

// logMessage logs a message if the current log level is sufficient
func logMessage(level LogLevel, format string, v ...interface{}) {
	if level <= logLevel {
		log.Printf(format, v...)
	}
}

// RegisterConfig represents the configuration for a register
type RegisterConfig struct {
	Name         string `json:"name"`
	Format       string `json:"format"` // "decimal", "hex", "float", "boolean", "string-byte", "string-word"
	Address      uint16 `json:"address"`
	StringLength int    `json:"stringLength,omitempty"`
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
	ID               string                    `json:"id"`
	Address          string                    `json:"address"`
	Port             int                       `json:"port"`
	PollRate         int                       `json:"pollRate"`
	RegisterBlocks   []RegisterBlock           `json:"registerBlocks"`
	client           *ModbusClient             `json:"-"`
	mu               sync.Mutex                `json:"-"`
	registerMap      map[uint16]RegisterConfig `json:"-"`
	dataModel        ModbusDataModel           `json:"-"`
	ConnectionStatus string                    `json:"connectionStatus"` // "ok" or "error"
	ConnectionError  string                    `json:"connectionError,omitempty"`
	LastDataReceived time.Time                 `json:"lastDataReceived"`
}

// HTML templates
const (
	serverListTemplate = `
		{{define "serverList"}}
			{{range .}}
			<div class="card mb-4" id="server-{{.ID}}">
				<div class="card-header d-flex justify-content-between align-items-center">
					<div class="d-flex align-items-center">
						<!-- Status dot -->
						<span style="display:inline-block;width:12px;height:12px;border-radius:50%;margin-right:8px;vertical-align:middle;background-color:{{if eq .ConnectionStatus "ok"}}#28a745{{else}}#dc3545{{end}};border:1px solid #888;"></span>
						<button class="btn btn-sm btn-outline-secondary me-2" onclick="toggleServerTable('{{.ID}}')">
							<span id="toggle-icon-{{.ID}}">▼</span>
						</button>
						<div>
							<h5 class="mb-0">Server: {{.ID}}</h5>
							<div hx-get="/api/serverstatus/{{.ID}}" hx-target="#server-{{.ID}}-status" hx-swap="innerHTML" hx-trigger="load, every 1s" id="server-{{.ID}}-status"></div>
						</div>
					</div>
					<div>
						<button class="btn btn-info btn-sm me-2" onclick="showAddBlockModal('{{.ID}}')" data-server-id="{{.ID}}">
							<i class="bi bi-plus-circle"></i> Add Block
						</button>
						<button class="btn btn-info btn-sm me-2" onclick="showAddRegisterModal('{{.ID}}')" data-server-id="{{.ID}}">
							<i class="bi bi-plus-circle"></i> Add Register
						</button>
						<button class="btn btn-info btn-sm me-2" onclick="showBulkAddModal('{{.ID}}')" data-server-id="{{.ID}}">
							<i class="bi bi-plus-circle"></i> Bulk Add
						</button>
						<button class="btn btn-danger btn-sm" 
								hx-delete="/api/servers/{{.ID}}"
								hx-confirm="Are you sure you want to remove server {{.ID}}?"
								hx-target="#server-{{.ID}}"
								hx-swap="outerHTML swap:1s">Remove</button>
					</div>
				</div>
				<div class="card-body" id="server-content-{{.ID}}">
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
			{{end}}
		{{end}}`

	registerTableTemplate = `
		{{define "registerTable"}}
		{{range .Data}}
		<tr>
			<td>{{.Address}}</td>
			<td>{{.Name}}</td>
			<td class="register-value">{{.Value}}</td>
			<td>{{.Format}}</td>
		</tr>
		{{end}}
		<tr>
			<td colspan="4" style="display:none;" id="last-data-{{.ServerID}}">{{.LastDataReceived.Format "15:04:05.000"}}</td>
		</tr>
		{{end}}
`

	serverStatusTemplate = `{{define "serverStatus"}}<small class="text-muted">IP: {{.Address}} | Port: {{.Port}} | Poll: {{.PollRate}} ms | Last Data Received: {{.LastDataReceived.Format "15:04:05.000"}}</small></div>{{end}}`
)

// serveStaticFile serves a file from the embedded filesystem with the correct MIME type
func serveStaticFile(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/static/")

	// Get the file extension and set the correct MIME type
	ext := filepath.Ext(path)
	mimeType := mime.TypeByExtension(ext)
	if mimeType != "" {
		w.Header().Set("Content-Type", mimeType)
	}

	// Serve the file from the static directory
	http.FileServer(http.FS(staticFiles)).ServeHTTP(w, r)
}

func main() {
	// Parse templates once at startup
	templates = template.Must(template.New("serverStatus").Parse(serverStatusTemplate))
	templates = template.Must(templates.Parse(serverListTemplate))
	templates = template.Must(templates.Parse(registerTableTemplate))

	// Custom usage message
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "Modbus Browser v0.1.0\n")
		fmt.Fprintf(flag.CommandLine.Output(), "A web-based Modbus client for monitoring and configuring Modbus devices\n\n")
		fmt.Fprintf(flag.CommandLine.Output(), "GitHub: https://github.com/rustyoz/modbusbrowser\n")
		fmt.Fprintf(flag.CommandLine.Output(), "Author: https://github.com/rustyoz\n")
		fmt.Fprintf(flag.CommandLine.Output(), "Issues: https://github.com/rustyoz/modbusbrowser/issues\n")
		fmt.Fprintf(flag.CommandLine.Output(), "License: https://github.com/rustyoz/modbusbrowser/blob/main/LICENSE\n\n")

		fmt.Fprintf(flag.CommandLine.Output(), "Usage of %s:\n", os.Args[0])
		fmt.Fprintf(flag.CommandLine.Output(), "\nOptions:\n")
		flag.PrintDefaults()
		fmt.Fprintf(flag.CommandLine.Output(), "\nLog Levels:\n")
		fmt.Fprintf(flag.CommandLine.Output(), "  error  - Only show error messages (default)\n")
		fmt.Fprintf(flag.CommandLine.Output(), "  info   - Show error and info messages\n")
		fmt.Fprintf(flag.CommandLine.Output(), "  debug  - Show all messages including debug\n")
		fmt.Fprintf(flag.CommandLine.Output(), "\nExamples:\n")
		fmt.Fprintf(flag.CommandLine.Output(), "  %s -port 8080 -log-level debug\n", os.Args[0])
		fmt.Fprintf(flag.CommandLine.Output(), "  %s -port 9000 -log-level info\n", os.Args[0])
	}

	// Parse command line flags
	port := flag.Int("port", 8080, "Port to start the server on")
	logLevelStr := flag.String("log-level", "error", "Log level (error, info, debug)")
	flag.Parse()

	// Set log level
	switch strings.ToLower(*logLevelStr) {
	case "debug":
		logLevel = DebugLevel
	case "info":
		logLevel = InfoLevel
	default:
		logLevel = ErrorLevel
	}

	// Print intro message without logging
	fmt.Println("Modbus Browser v0.1.0")
	fmt.Println("https://github.com/rustyoz/modbusbrowser")
	fmt.Println("https://github.com/rustyoz")
	fmt.Println("https://github.com/rustyoz/modbusbrowser/issues")
	fmt.Println("https://github.com/rustyoz/modbusbrowser/releases")
	fmt.Println("https://github.com/rustyoz/modbusbrowser/blob/main/LICENSE")
	fmt.Println("https://github.com/rustyoz/modbusbrowser/blob/main/README.md")

	// Serve static files from embedded filesystem with correct MIME types
	http.HandleFunc("/static/", serveStaticFile)
	http.Handle("/", http.HandlerFunc(ServeIndex))

	http.HandleFunc("/api/servers/config/", handleServerConfig)
	http.HandleFunc("/api/servers/", handleServer)
	http.HandleFunc("/api/servers", handleServers)
	http.HandleFunc("/api/config/upload", handleConfigUpload)
	http.HandleFunc("/api/config", handleGetConfig)
	http.HandleFunc("/api/serverstatus/", handleServerStatus)

	logMessage(ErrorLevel, "Starting server on port %d...", *port)
	if err := http.ListenAndServe(fmt.Sprintf(":%d", *port), nil); err != nil {
		log.Fatal(err)
	}
}

func handleServers(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		// Return list of servers
		mu.RLock()
		var serverList []map[string]interface{}
		serverList = make([]map[string]interface{}, 0, len(servers))
		for id, srv := range servers {
			srv.mu.Lock()
			serverList = append(serverList, map[string]interface{}{
				"ID":               id,
				"ConnectionStatus": srv.ConnectionStatus,
				"ConnectionError":  srv.ConnectionError,
				"Address":          srv.Address,
				"Port":             srv.Port,
				"PollRate":         srv.PollRate,
				"LastDataReceived": srv.LastDataReceived,
			})
			srv.mu.Unlock()
		}
		mu.RUnlock()

		if isHtmxRequest(r) {
			w.Header().Set("Content-Type", "text/html")
			if err := templates.ExecuteTemplate(w, "serverList", serverList); err != nil {
				handleError(w, r, fmt.Sprintf("Error executing template: %v", err))
				return
			}
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
			ID:               config.ID,
			Address:          config.Address,
			Port:             config.Port,
			PollRate:         config.PollRate,
			registerMap:      make(map[uint16]RegisterConfig),
			dataModel:        dataModel,
			ConnectionStatus: "error", // default to error until connected
		}

		// Try to create Modbus client
		client, err := NewModbusClient(server.Address, server.Port)
		if err == nil {
			server.client = client
			server.ConnectionStatus = "ok"
			server.ConnectionError = ""
		} else {
			server.ConnectionStatus = "error"
			server.ConnectionError = err.Error()
			// Start retry goroutine
			go func(s *ModbusServer) {
				for {
					time.Sleep(1 * time.Second)
					client, err := NewModbusClient(s.Address, s.Port)
					if err == nil {
						s.mu.Lock()
						s.client = client
						s.ConnectionStatus = "ok"
						s.ConnectionError = ""
						s.mu.Unlock()
						break
					} else {
						s.mu.Lock()
						s.ConnectionStatus = "error"
						s.ConnectionError = err.Error()
						s.mu.Unlock()
					}
				}
			}(server)
		}

		// Add server to map
		mu.Lock()
		servers[server.ID] = server
		mu.Unlock()

		// Start polling in background
		go pollServer(server)

		w.WriteHeader(http.StatusCreated)
		if isHtmxRequest(r) {
			w.Header().Set("HX-Trigger", "load")
			w.Header().Set("Content-Type", "text/html")
			if err := templates.ExecuteTemplate(w, "serverList", []map[string]interface{}{{
				"ID":               server.ID,
				"ConnectionStatus": server.ConnectionStatus,
				"ConnectionError":  server.ConnectionError,
				"Address":          server.Address,
				"Port":             server.Port,
				"PollRate":         server.PollRate,
				"LastDataReceived": server.LastDataReceived,
			}}); err != nil {
				handleError(w, r, fmt.Sprintf("Error executing template: %v", err))
				return
			}
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
		for _, block := range server.RegisterBlocks {

			// log the block details
			logMessage(DebugLevel, "block: %+v", block)

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
				case "string-byte":
					// For string-byte format, we need to read multiple registers and combine them
					if addr < 40000 { // input registers
						registers := make([]uint16, regConfig.StringLength/2+1)
						for j := uint16(0); j < uint16(len(registers)); j++ {
							if addr+j >= block.StartAddress+block.Length {
								registers[j] = 0
							} else {
								registers[j] = server.dataModel.InputRegisters[addr+j-30000]
							}
						}
						// Convert registers to bytes and then to string
						bytes := make([]byte, 0, regConfig.StringLength)
						for _, reg := range registers {
							bytes = append(bytes, byte(reg>>8), byte(reg))
						}
						// Trim null bytes and convert to string
						displayValue = strings.TrimRight(string(bytes), "\x00")
					} else { // holding registers
						registers := make([]uint16, regConfig.StringLength/2+1)
						for j := uint16(0); j < uint16(len(registers)); j++ {
							if addr+j >= block.StartAddress+block.Length {
								registers[j] = 0
							} else {
								registers[j] = server.dataModel.HoldingRegisters[addr+j-40000]
							}
						}
						// Convert registers to bytes and then to string
						bytes := make([]byte, 0, regConfig.StringLength)
						for _, reg := range registers {
							bytes = append(bytes, byte(reg>>8), byte(reg))
						}
						// Trim null bytes and convert to string
						displayValue = strings.TrimRight(string(bytes), "\x00")
					}
					i = i + uint16(regConfig.StringLength/2)
				case "string-word":
					// For string-word format, each register represents one character
					if addr < 40000 { // input registers
						registers := make([]uint16, regConfig.StringLength)
						for j := uint16(0); j < uint16(len(registers)); j++ {
							if addr+j >= block.StartAddress+block.Length {
								registers[j] = 0
							} else {
								registers[j] = server.dataModel.InputRegisters[addr+j-30000]
							}
						}
						// Convert registers to characters
						chars := make([]rune, 0, regConfig.StringLength)
						for _, reg := range registers {
							chars = append(chars, rune(reg))
						}
						displayValue = string(chars)
					} else { // holding registers
						registers := make([]uint16, regConfig.StringLength)
						for j := uint16(0); j < uint16(len(registers)); j++ {
							if addr+j >= block.StartAddress+block.Length {
								registers[j] = 0
							} else {
								registers[j] = server.dataModel.HoldingRegisters[addr+j-40000]
							}
						}
						// Convert registers to characters
						chars := make([]rune, 0, regConfig.StringLength)
						for _, reg := range registers {
							chars = append(chars, rune(reg))
						}
						displayValue = string(chars)
					}
					i = i + uint16(regConfig.StringLength-1)
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
			w.Header().Set("Content-Type", "text/html")
			if err := templates.ExecuteTemplate(w, "registerTable", map[string]interface{}{
				"Data":             data,
				"ServerID":         id,
				"LastDataReceived": server.LastDataReceived,
			}); err != nil {
				handleError(w, r, fmt.Sprintf("Error executing template: %v", err))
				return
			}
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
	logMessage(DebugLevel, "handleConfigUpload: %s %s", r.Method, r.URL.Path)

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
			logMessage(ErrorLevel, "Invalid JSON: %v", err)
			return
		}
	} else {
		// Handle direct JSON
		if err := json.NewDecoder(r.Body).Decode(&config); err != nil {
			handleError(w, r, fmt.Sprintf("Invalid JSON: %v", err))
			logMessage(ErrorLevel, "Invalid JSON: %v", err)
			return
		}
	}

	logMessage(DebugLevel, "config: %+v", config)

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

		// Create Modbus client
		client, err := NewModbusClient(server.Address, server.Port)
		if err != nil {
			handleError(w, r, fmt.Sprintf("Failed to create Modbus client for server %s: %v", server.ID, err))
			logMessage(ErrorLevel, "Failed to create Modbus client for server %s: %v", server.ID, err)
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

	if isHtmxRequest(r) {
		w.Header().Set("HX-Trigger", "load")
		w.Header().Set("Content-Type", "text/html")
		if err := templates.ExecuteTemplate(w, "serverList", config.Servers); err != nil {
			handleError(w, r, fmt.Sprintf("Error executing template: %v", err))
			return
		}
	} else {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
		})
	}
}

// handleGetConfig returns the current server configuration
func handleGetConfig(w http.ResponseWriter, r *http.Request) {
	logMessage(DebugLevel, "handleGetConfig: %s %s", r.Method, r.URL.Path)

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
		logMessage(ErrorLevel, "Error encoding config: %v", err)
	}
}

// Add new handlers for modifying server configuration
func handleServerConfig(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 4 {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}
	serverID := parts[4]

	mu.RLock()
	server, exists := servers[serverID]
	mu.RUnlock()

	if !exists {
		handleError(w, r, fmt.Sprintf("Server not found: %s", serverID))
		return
	}

	switch r.Method {

	case http.MethodGet:
		server.mu.Lock()
		json.NewEncoder(w).Encode(server)
		server.mu.Unlock()

	case http.MethodPost:
		var config ModbusServer

		if err := json.NewDecoder(r.Body).Decode(&config); err != nil {
			handleError(w, r, fmt.Sprintf("Invalid request body: %v", err))
			return
		}

		server.mu.Lock()
		// Update register map
		registerMap := make(map[uint16]RegisterConfig)
		for _, block := range config.RegisterBlocks {
			for _, reg := range block.Registers {
				registerMap[reg.Address] = reg
			}
		}
		server.registerMap = registerMap

		// Process new blocks and merge with existing ones
		var newBlocks []RegisterBlock
		for _, newBlock := range config.RegisterBlocks {
			merged := false

			// Try to merge with existing blocks
			for i, existingBlock := range server.RegisterBlocks {
				// Check if blocks overlap or are adjacent
				if newBlock.StartAddress >= existingBlock.StartAddress &&
					newBlock.StartAddress <= existingBlock.StartAddress+existingBlock.Length {

					// Calculate total length needed
					endAddr := newBlock.StartAddress + newBlock.Length
					existingEndAddr := existingBlock.StartAddress + existingBlock.Length
					totalLength := uint16(math.Max(float64(endAddr), float64(existingEndAddr))) - existingBlock.StartAddress

					if totalLength <= 125 {
						// Merge blocks
						server.RegisterBlocks[i].Length = totalLength
						server.RegisterBlocks[i].Registers = append(server.RegisterBlocks[i].Registers, newBlock.Registers...)
						merged = true
						break
					}
				}
			}

			if !merged {
				// Split block if length > 125
				remaining := newBlock.Length
				currentAddr := newBlock.StartAddress
				currentRegIdx := 0

				for remaining > 0 {
					length := uint16(math.Min(float64(remaining), 125))

					// Create new block
					block := RegisterBlock{
						StartAddress: currentAddr,
						Length:       length,
					}

					// Add registers that fall within this block
					for i := currentRegIdx; i < len(newBlock.Registers); i++ {
						reg := newBlock.Registers[i]
						if reg.Address >= currentAddr && reg.Address < currentAddr+length {
							block.Registers = append(block.Registers, reg)
							currentRegIdx = i + 1
						}
					}

					newBlocks = append(newBlocks, block)
					remaining -= length
					currentAddr += length
				}
			}
		}

		// Add any new blocks that couldn't be merged
		server.RegisterBlocks = append(server.RegisterBlocks, newBlocks...)

		server.mu.Unlock()

		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
		})

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
		if server.client == nil {
			server.ConnectionStatus = "error"
			server.ConnectionError = "not connected"
			server.mu.Unlock()
			continue
		}
		// Process each register block
		for _, block := range server.RegisterBlocks {
			switch {
			case block.StartAddress < 10000: // Coils
				values, err := server.client.ReadCoils(block.StartAddress, block.Length)
				if err != nil {
					server.ConnectionStatus = "error"
					server.ConnectionError = err.Error()
					server.mu.Unlock()
					// Start retry goroutine if not already retrying
					go retryConnection(server)
					return
				}
				copy(server.dataModel.Coils[block.StartAddress:block.StartAddress+block.Length], values)
			case block.StartAddress < 20000: // Discrete Inputs
				values, err := server.client.ReadDiscreteInputs(block.StartAddress-10000, block.Length)
				if err != nil {
					server.ConnectionStatus = "error"
					server.ConnectionError = err.Error()
					server.mu.Unlock()
					go retryConnection(server)
					return
				}
				copy(server.dataModel.DiscreteInputs[block.StartAddress-10000:block.StartAddress-10000+block.Length], values)
			case block.StartAddress < 40000: // Input Registers
				values, err := server.client.ReadInputRegisters(block.StartAddress-30000, block.Length)
				if err != nil {
					server.ConnectionStatus = "error"
					server.ConnectionError = err.Error()
					server.mu.Unlock()
					go retryConnection(server)
					return
				}
				copy(server.dataModel.InputRegisters[block.StartAddress-30000:block.StartAddress-30000+block.Length], values)
			default: // Holding Registers
				values, err := server.client.ReadHoldingRegisters(block.StartAddress-40000, block.Length)
				if err != nil {
					server.ConnectionStatus = "error"
					server.ConnectionError = err.Error()
					server.mu.Unlock()
					go retryConnection(server)
					return
				}
				copy(server.dataModel.HoldingRegisters[block.StartAddress-40000:block.StartAddress-40000+block.Length], values)
			}
			// Set last data received time after successful read
			server.LastDataReceived = time.Now()
		}
		server.ConnectionStatus = "ok"
		server.ConnectionError = ""
		server.mu.Unlock()
	}
}

// retryConnection tries to reconnect every second until successful, then restarts polling
func retryConnection(server *ModbusServer) {
	for {
		time.Sleep(1 * time.Second)
		client, err := NewModbusClient(server.Address, server.Port)
		server.mu.Lock()
		if err == nil {
			server.client = client
			server.ConnectionStatus = "ok"
			server.ConnectionError = ""
			server.mu.Unlock()
			go pollServer(server)
			return
		} else {
			server.ConnectionStatus = "error"
			server.ConnectionError = err.Error()
		}
		server.mu.Unlock()
	}
}

// handleServerStatus serves the status line for a server
func handleServerStatus(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 4 {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}
	id := parts[3]
	mu.RLock()
	server, exists := servers[id]
	mu.RUnlock()
	if !exists {
		http.Error(w, "Server not found", http.StatusNotFound)
		return
	}
	server.mu.Lock()
	defer server.mu.Unlock()
	w.Header().Set("Content-Type", "text/html")
	if err := templates.ExecuteTemplate(w, "serverStatus", server); err != nil {
		handleError(w, r, fmt.Sprintf("Error executing template: %v", err))
		return
	}
}
