# Modbus Browser

A modern web application for monitoring and interacting with Modbus servers. Built with Go and HTMX, this application provides a user-friendly interface to connect to multiple Modbus servers simultaneously and monitor their registers in real-time.

## Features

- **Multi-Server Support**: Connect to and monitor multiple Modbus servers simultaneously
- **Real-Time Monitoring**: Live updates of register values with configurable polling rates
- **Data Visualization**: View register values in both decimal and hexadecimal formats
- **Modern UI**: Clean, responsive web interface built with HTMX
- **Easy Configuration**: Simple setup process for adding new Modbus servers
- **Cross-Platform**: Works on Windows, Linux, and macOS

## Prerequisites

- Go 1.21 or later
- A modern web browser (Chrome, Firefox, Safari, or Edge recommended)
- Network access to Modbus servers

## Installation

### Windows

1. Install Go:
   - Download the latest Go installer from [golang.org/dl](https://golang.org/dl/)
   - Run the installer and follow the installation wizard
   - Verify installation by opening Command Prompt and running:
     ```cmd
     go version
     ```

2. Clone the repository:
   ```cmd
   git clone https://github.com/rustyoz/modbusbrowser.git
   cd modbusbrowser
   ```

3. Install dependencies:
   ```cmd
   go mod tidy
   ```

4. Build the application:
   ```cmd
   go build -o modbusbrowser.exe
   ```

5. Run the application:
   ```cmd
   modbusbrowser.exe
   ```
   
   To see all available options:
   ```cmd
   modbusbrowser.exe -help
   ```
   
   By default, the application runs on port 8080. To use a different port, specify it using the `-port` flag:
   ```cmd
   modbusbrowser.exe -port 3000
   ```

6. Open your browser and navigate to `http://localhost:8080` (or your specified port)

### Linux/macOS

1. Install Go:
   - Follow the installation instructions at [golang.org/doc/install](https://golang.org/doc/install)
   - Verify installation with `go version`

2. Clone the repository:
   ```bash
   git clone https://github.com/rustyoz/modbusbrowser.git
   cd modbusbrowser
   ```

3. Install dependencies:
   ```bash
   go mod tidy
   ```

4. Build the application:
   ```bash
   go build -o modbusbrowser
   ```

5. Run the application:
   ```bash
   ./modbusbrowser
   ```
   
   To see all available options:
   ```bash
   ./modbusbrowser -help
   ```
   
   By default, the application runs on port 8080. To use a different port, specify it using the `-port` flag:
   ```bash
   ./modbusbrowser -port 3000
   ```

6. Open your browser and navigate to `http://localhost:8080` (or your specified port)

## Application Details

The Modbus Browser is a single binary application that includes all necessary components:
- The Go server application
- Embedded static files (HTML, CSS, JavaScript)
- All required dependencies

This means you can distribute and run the application without needing to manage separate static files or dependencies. The application is completely self-contained after building.

### Command Line Options

The application supports the following command line options:

- `-help`: Display help information and available options
- `-port`: Specify the port number to run the server on (default: 8080)

Example usage:
```bash
# Show help
./modbusbrowser -help

# Run on custom port
./modbusbrowser -port 3000
```

## Usage

### Adding a Modbus Server

1. Click the "Add Server" button on the main interface
2. Fill out the server configuration form:
   - **Server ID**: A unique identifier for the server (e.g., "PLC1", "SensorHub")
   - **Address**: IP address or hostname of the Modbus server
   - **Port**: Modbus port (default: 502)
   - **Poll Rate**: How often to poll the server in milliseconds (recommended: 1000-5000)
   - **Start Address**: The first register address to monitor
   - **Number of Registers**: How many consecutive registers to monitor

### Monitoring Registers

- Each server's registers are displayed in a card format
- Register values are automatically updated at the specified polling rate
- For each register, you can see:
  - Register address
  - Current value in decimal
  - Current value in hexadecimal
- Use the "Remove" button to disconnect from a server

### Best Practices

1. Start with a higher poll rate (e.g., 5000ms) and adjust based on your needs
2. Monitor only the registers you need to reduce network traffic
3. Use descriptive server IDs to easily identify different devices
4. Keep the number of monitored registers reasonable to maintain performance

## Troubleshooting

### Common Issues

1. **Go Installation Issues**:
   - If you get "command not found" errors:
     - Make sure Go is added to your PATH
     - Try restarting your terminal
     - Verify Go installation with `go version`

2. **Application Startup Issues**:
   - If the server won't start:
     - Check if the specified port is already in use
     - Try using a different port with the `-port` flag
     - Verify you have the necessary permissions
     - Check the application logs for error messages

3. **Modbus Connection Issues**:
   - If you can't connect to a Modbus server:
     - Verify the server address and port are correct
     - Check if the Modbus server is running and accessible
     - Ensure your firewall allows the connection
     - Verify network connectivity between the application and Modbus server

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

## License

MIT License - See the [LICENSE](LICENSE) file for details. 