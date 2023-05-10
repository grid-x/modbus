# go modbus
Fault-tolerant, fail-fast implementation of Modbus protocol in Go.

# Supported functions

Bit access:
- Read Discrete Inputs
- Read Coils
- Write Single Coil
- Write Multiple Coils

16-bit access:
- Read Input Registers
- Read Holding Registers
- Write Single Register
- Write Multiple Registers
- Read/Write Multiple Registers
- Mask Write Register
- Read FIFO Queue

# Supported formats
- TCP
- Serial (RTU, ASCII)

# Usage
Basic usage:
```go
// Modbus TCP
client := modbus.TCPClient("localhost:502")
// Read input register 9
results, err := client.ReadInputRegisters(8, 1)

// Modbus RTU/ASCII
// Default configuration is 19200, 8, 1, even
client = modbus.RTUClient("/dev/ttyS0")
results, err = client.ReadCoils(2, 1)
```

Advanced usage:
```go
// Modbus TCP
handler := modbus.NewTCPClientHandler("localhost:502")
handler.Timeout = 10 * time.Second
handler.SlaveID = 0xFF
handler.Logger = log.New(os.Stdout, "test: ", log.LstdFlags)
// Connect manually so that multiple requests are handled in one connection session
err := handler.Connect()
defer handler.Close()

client := modbus.NewClient(handler)
results, err := client.ReadDiscreteInputs(15, 2)
results, err = client.WriteMultipleRegisters(1, 2, []byte{0, 3, 0, 4})
results, err = client.WriteMultipleCoils(5, 10, []byte{4, 3})
```

```go
// Modbus RTU/ASCII
handler := modbus.NewRTUClientHandler("/dev/ttyUSB0")
handler.BaudRate = 115200
handler.DataBits = 8
handler.Parity = "N"
handler.StopBits = 1
handler.SlaveID = 1
handler.Timeout = 5 * time.Second

err := handler.Connect()
defer handler.Close()

client := modbus.NewClient(handler)
results, err := client.ReadDiscreteInputs(15, 2)
```

# Modbus-CLI

We offer a CLI tool to read/write registers.

## Usage

For simplicity, the following examples are all using Modbus TCP.
For Modbus RTU, replace the address field and use the `rtu-` arguments in order to use different baudrates, databits, etc.
```sh
./modbus-cli -address=rtu:///dev/ttyUSB0 -rtu-baudrate=57600 -rtu-stopbits=2 -rtu-parity=N -rtu-databits=8 ...
```
### Reading Registers

Read 1 register and get raw result
```sh
./modbus-cli -address=tcp://127.0.0.1:502 -quantity=1 -type-parse=raw -register=42
```

Read 1 register and decode result as uint16
```sh
./modbus-cli -address=tcp://127.0.0.1:502 -quantity=1 -type-parse=uint16 -register=42
```

Read 1 register and get all possible decoded results
```sh
./modbus-cli -address=tcp://127.0.0.1:502 -quantity=1 -type-parse=all -register=42
```

Read 2 registers and decode result as uint32
```sh
./modbus-cli -address=tcp://127.0.0.1:502 -quantity=2 -type-parse=uint32 -register=42
```

Read 2 registers and get all possible decoded results
```sh
./modbus-cli -address=tcp://127.0.0.1:502 -quantity=2 -type-parse=all -register=42
```

Reading multiple registers is only possible in the raw format
```sh
./modbus-cli -address=tcp://127.0.0.1:502 -quantity=16 -type-parse=raw -register=42
```

### Writing Registers

Write 1 register 
```sh
./modbus-cli -address=tcp://127.0.0.1:502 -fn-code=0x06 -type-exec=uint16 -register=42 -write-value=7
```

Write 2 registers
```sh
./modbus-cli -address=tcp://127.0.0.1:502 -fn-code=0x10 -type-exec=uint32 -register=42 -write-value=7
```

## Release

To release the Modbus-CLI tool, run either `make release` if you have installed `goreleaser` or `make ci_release`.
The generated files can be found in the directory in the `dist` directory.

Take the `.tar.gz` and `.zip` files and create a new GitHub release.

# References
- [Modbus Specifications and Implementation Guides](http://www.modbus.org/specs.php)
