package main

import (
	"fmt"
	"time"

	"github.com/rustyoz/modbus"
)

// ModbusClient represents a connection to a Modbus server
type ModbusClient struct {
	handler *modbus.TCPClientHandler
	client  modbus.Client
}

// NewModbusClient creates a new Modbus client
func NewModbusClient(address string, port int) (*ModbusClient, error) {
	handler := modbus.NewTCPClientHandler(fmt.Sprintf("%s:%d", address, port))
	handler.Timeout = 10 * time.Second
	handler.SlaveId = 1

	err := handler.Connect()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Modbus server: %v", err)
	}

	client := modbus.NewClient(handler)

	return &ModbusClient{
		handler: handler,
		client:  client,
	}, nil
}

// Close closes the Modbus connection
func (c *ModbusClient) Close() {
	if c.handler != nil {
		c.handler.Close()
	}
}

// ReadRegister reads a single register
func (c *ModbusClient) ReadRegister(address uint16) (uint16, error) {
	results, err := c.client.ReadHoldingRegisters(address, 1)
	if err != nil {
		return 0, err
	}
	return uint16(results[0])<<8 | uint16(results[1]), nil
}

// ReadCoil reads a single coil
func (c *ModbusClient) ReadCoil(address uint16) (bool, error) {
	results, err := c.client.ReadCoils(address, 1)
	if err != nil {
		return false, err
	}
	return results[0] == 1, nil
}

// ReadHoldingRegisters reads multiple holding registers
func (c *ModbusClient) ReadHoldingRegisters(address uint16, quantity uint16) ([]interface{}, error) {
	results, err := c.client.ReadHoldingRegisters(address, quantity)
	if err != nil {
		return nil, err
	}

	registers := make([]interface{}, quantity)
	for i := uint16(0); i < quantity; i++ {
		registers[i] = uint16(results[i*2])<<8 | uint16(results[i*2+1])
	}
	return registers, nil
}

// ReadInputRegisters reads multiple input registers
func (c *ModbusClient) ReadInputRegisters(address uint16, quantity uint16) ([]interface{}, error) {
	results, err := c.client.ReadInputRegisters(address, quantity)
	if err != nil {
		return nil, err
	}

	registers := make([]interface{}, quantity)
	for i := uint16(0); i < quantity; i++ {
		registers[i] = uint16(results[i*2])<<8 | uint16(results[i*2+1])
	}
	return registers, nil
}

// ReadCoils reads multiple coils
func (c *ModbusClient) ReadCoils(address uint16, quantity uint16) ([]interface{}, error) {
	results, err := c.client.ReadCoils(address, quantity)
	if err != nil {
		return nil, err
	}

	coils := make([]interface{}, quantity)
	for i := uint16(0); i < quantity; i++ {
		coils[i] = results[i] == 1
	}
	return coils, nil
}

// ReadDiscreteInputs reads multiple discrete inputs
func (c *ModbusClient) ReadDiscreteInputs(address uint16, quantity uint16) ([]interface{}, error) {
	results, err := c.client.ReadDiscreteInputs(address, quantity)
	if err != nil {
		return nil, err
	}

	inputs := make([]interface{}, quantity)
	for i := uint16(0); i < quantity; i++ {
		inputs[i] = results[i] == 1
	}
	return inputs, nil
}
