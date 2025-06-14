# Modbus Browser

A web application for monitoring Modbus servers. Built with Go and HTMX, this application allows you to connect to multiple Modbus servers and monitor their registers in real-time.

## Features

- Connect to multiple Modbus servers simultaneously
- Monitor Modbus registers with real-time updates
- Configurable polling rates
- Modern, responsive web interface
- Simple and intuitive user interface
- Real-time data updates using HTMX

## Prerequisites

- Go 1.21 or later
- A modern web browser

## Building

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

4. Build and run the application:
   ```cmd
   go run main.go
   ```

5. Open your browser and navigate to `http://localhost:8080`

### Linux/macOS

1. Clone the repository:
```bash
git clone https://github.com/rustyoz/modbusbrowser.git
cd modbusbrowser
```

2. Build and run the application:
```bash
go run main.go
```

## Usage

1. Add a new server by filling out the form:
   - Server ID: A unique identifier for the server
   - Address: IP address or hostname of the Modbus server
   - Port: Modbus port (default: 502)
   - Poll Rate: How often to poll the server in milliseconds
   - Start Address: The first register address to monitor
   - Number of Registers: How many consecutive registers to monitor

2. The data will automatically update at the specified polling rate, showing:
   - Register address
   - Decimal value
   - Hexadecimal value

3. You can remove servers using the "Remove" button on each server card.

## Troubleshooting

### Common Issues

1. If you get "command not found" errors:
   - Make sure Go is added to your PATH
   - Try restarting your terminal
   - Verify Go installation with `go version`

2. If the server won't start:
   - Check if port 8080 is already in use
   - Verify you have the necessary permissions
   - Check the application logs for error messages

3. If you can't connect to a Modbus server:
   - Verify the server address and port are correct
   - Check if the Modbus server is running and accessible
   - Ensure your firewall allows the connection

## License

MIT License 