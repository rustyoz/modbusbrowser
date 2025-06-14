module github.com/rustyoz/modbusbrowser

go 1.23.0

require github.com/rustyoz/modbus v0.0.0-20240218000000-000000000000

require github.com/rustyoz/serial v0.1.0 // indirect

replace github.com/rustyoz/modbus => ../modbus

replace github.com/rustyoz/serial => ../serial
