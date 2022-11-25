package main

import (
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"math"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/grid-x/modbus"
	"github.com/grid-x/serial"
)

func main() {
	var opt option
	// general
	flag.StringVar(&opt.address, "address", "tcp://127.0.0.1:502", "Example: tcp://127.0.0.1:502, rtu:///dev/ttyUSB0")
	flag.IntVar(&opt.slaveID, "slaveID", 1, "Is used for intra-system routing purpose, typically for serial connections, TCP default 0xFF")
	flag.DurationVar(&opt.timeout, "timeout", 20*time.Second, "Modbus connection timeout")
	// tcp
	flag.DurationVar(&opt.tcp.linkRecoveryTimeout, "tcp-timeout-link-recovery", 20*time.Second, "Link timeout")
	flag.DurationVar(&opt.tcp.protocolRecoveryTimeout, "tcp-timeout-protocol-recovery", 20*time.Second, "Proto timeout")
	// rtu
	flag.IntVar(&opt.rtu.baudrate, "rtu-baudrate", 2400, "Symbol rate, e.g.: 300, 600, 1200, 2400, 4800, 9600, 19200, 38400")
	flag.IntVar(&opt.rtu.dataBits, "rtu-databits", 8, "5, 6, 7 or 8")
	flag.StringVar(&opt.rtu.parity, "rtu-parity", "E", "Parity: N - None, E - Even, O - Odd")
	flag.IntVar(&opt.rtu.stopBits, "rtu-stopbits", 1, "1 or 2")
	// rs485
	flag.BoolVar(&opt.rtu.rs485.enabled, "rs485-enable", false, "enables rs485 cfg")
	flag.DurationVar(&opt.rtu.rs485.delayRtsBeforeSend, "rs485-delayRtsBeforeSend", 0, "Delay rts before send")
	flag.DurationVar(&opt.rtu.rs485.delayRtsBeforeSend, "rs485-delayRtsAfterSend", 0, "Delay rts after send")
	flag.BoolVar(&opt.rtu.rs485.rtsHighDuringSend, "rs485-rtsHighDuringSend", false, "Allow rts high during send")
	flag.BoolVar(&opt.rtu.rs485.rtsHighAfterSend, "rs485-rtsHighAfterSend", false, "Allow rts high after send")
	flag.BoolVar(&opt.rtu.rs485.rxDuringTx, "rs485-rxDuringTx", false, "Allow bidirectional rx during tx")

	var (
		register       = flag.Int("register", -1, "")
		fnCode         = flag.Int("fn-code", 0x03, "fn")
		quantity       = flag.Int("quantity", 2, "register quantity, length in bytes")
		ignoreCRCError = flag.Bool("ignore-crc", false, "ignore crc")
		eType          = flag.String("type-exec", "uint16", "")
		pType          = flag.String("type-parse", "raw", "type to parse the register result. Use 'raw' if you want to see the raw bits and bytes. Use 'all' if you want to decode the result to different commonly used formats.")
		writeValue     = flag.Float64("write-value", math.MaxFloat64, "")
		parseBigEndian = flag.Bool("order-parse-bigendian", true, "t: big, f: little")
		execBigEndian  = flag.Bool("order-exec-bigendian", true, "t: big, f: little")
		filename       = flag.String("filename", "", "")
		logframe       = flag.Bool("log-frame", false, "prints received and send modbus frame to stdout")
	)

	flag.Parse()

	if len(os.Args) == 1 {
		flag.PrintDefaults()
		return
	}

	logger := log.New(os.Stdout, "", 0)
	if *register > math.MaxUint16 || *register < 0 {
		logger.Fatalf("invalid register value: %d", *register)
	}

	startReg := uint16(*register)

	if *logframe {
		opt.logger = logger
	}

	var (
		eo binary.ByteOrder = binary.BigEndian
		po binary.ByteOrder = binary.BigEndian
	)
	if !*execBigEndian {
		eo = binary.LittleEndian
	}

	if !*parseBigEndian {
		po = binary.LittleEndian
	}

	handler, err := newHandler(opt)
	if err != nil {
		logger.Fatal(err)
	}
	if err := handler.Connect(); err != nil {
		log.Fatal(err)
	}
	defer handler.Close()

	client := modbus.NewClient(handler)

	result, err := exec(client, eo, *register, *fnCode, *writeValue, *eType, *quantity)
	if err != nil && strings.Contains(err.Error(), "crc") && *ignoreCRCError {
		logger.Printf("ignoring crc error: %+v\n", err)
	} else if err != nil {
		logger.Fatal(err)
	}

	var res string
	switch *pType {
	case "raw":
		res, err = resultToRawString(result, int(startReg))
	case "all":
		res, err = resultToAllString(result)
	default:
		res, err = resultToString(result, po, *pType)
	}

	if err != nil {
		logger.Fatal(err)
	}

	logger.Println(res)

	if *filename != "" {
		if err := resultToFile([]byte(res), *filename); err != nil {
			logger.Fatal(err)
		}
		logger.Printf("%s successfully written\n", *filename)
	}
}

func exec(
	client modbus.Client,
	o binary.ByteOrder,
	register int,
	fnCode int,
	wval float64,
	etype string,
	quantity int,
) (result []byte, err error) {
	switch fnCode {
	case 0x01:
		result, err = client.ReadCoils(uint16(register), uint16(quantity))
	case 0x05:
		const (
			coilOn  uint16 = 0xFF00
			coilOff uint16 = 0x0000
		)
		v := coilOff
		if wval > 0 {
			v = coilOn
		}
		result, err = client.WriteSingleCoil(uint16(register), v)
	case 0x06:
		max := float64(math.MaxUint16)
		if wval > max || wval < 0 {
			err = fmt.Errorf("overflow: %f does not fit into datatype uint16", wval)
			return
		}
		result, err = client.WriteSingleRegister(uint16(register), uint16(wval))
	case 0x10:
		w := newWriter(o)
		var buf []byte
		switch etype {
		case "uint16":
			max := float64(math.MaxUint16)
			if wval > max || wval < 0 {
				err = fmt.Errorf("overflow: %f does not fit into datatype %s", wval, etype)
				return
			}
			buf = make([]byte, 2)
			w.PutUint16(buf, uint16(wval))
		case "uint32":
			max := float64(math.MaxUint32)
			if wval > max || wval < 0 {
				err = fmt.Errorf("overflow: %f does not fit into datatype %s", wval, etype)
				return
			}
			buf = make([]byte, 4)
			w.PutUint32(buf, uint32(wval))
		case "float32":
			max := float64(math.MaxFloat32)
			if wval > max || wval < 0 {
				err = fmt.Errorf("overflow: %f does not fit into datatype %s", wval, etype)
				return
			}
			buf = make([]byte, 4)
			w.PutFloat32(buf, float32(wval))
		case "float64":
			buf = make([]byte, 8)
			w.PutFloat64(buf, float64(wval))
		}
		result, err = client.WriteMultipleRegisters(uint16(register), uint16(len(buf))/2, buf)
	case 0x04:
		result, err = client.ReadInputRegisters(uint16(register), uint16(quantity))
	case 0x03:
		result, err = client.ReadHoldingRegisters(uint16(register), uint16(quantity))
	default:
		err = fmt.Errorf("function code %d is unsupported", fnCode)
	}
	return
}

func resultToFile(r []byte, filename string) error {
	return ioutil.WriteFile(filename, r, 0644)
}

func resultToRawString(r []byte, startReg int) (string, error) {
	var res string
	for i := 0; i < len(r)/2; i++ {
		reg := startReg + i
		res += fmt.Sprintf("%d\t0x%X 0x%X\t %b %b\n", reg, r[i*2], r[i*2+1], r[i*2], r[i*2+1])
	}
	return res, nil
}

func resultToAllString(result []byte) (string, error) {
	switch len(result) {
	case 2:
		bigUint16, err := resultToString(result, binary.BigEndian, "uint16")
		if err != nil {
			return "", err
		}
		bigInt16, err := resultToString(result, binary.BigEndian, "int16")
		if err != nil {
			return "", err
		}
		littleUint16, err := resultToString(result, binary.LittleEndian, "uint16")
		if err != nil {
			return "", err
		}
		littleInt16, err := resultToString(result, binary.LittleEndian, "int16")
		if err != nil {
			return "", err
		}

		return strings.Join([]string{
			fmt.Sprintf("INT16  - Big Endian (AB):    %s", bigInt16),
			fmt.Sprintf("INT16  - Little Endian (BA): %s", littleInt16),
			fmt.Sprintf("UINT16 - Big Endian (AB):    %s", bigUint16),
			fmt.Sprintf("UINT16 - Little Endian (BA): %s", littleUint16),
		}, "\n"), nil
	case 4:
		bigUint32, err := resultToString(result, binary.BigEndian, "uint32")
		if err != nil {
			return "", err
		}
		bigInt32, err := resultToString(result, binary.BigEndian, "int32")
		if err != nil {
			return "", err
		}
		bigFloat32, err := resultToString(result, binary.BigEndian, "float32")
		if err != nil {
			return "", err
		}
		littleUint32, err := resultToString(result, binary.LittleEndian, "uint32")
		if err != nil {
			return "", err
		}
		littleInt32, err := resultToString(result, binary.LittleEndian, "int32")
		if err != nil {
			return "", err
		}
		littleFloat32, err := resultToString(result, binary.LittleEndian, "float32")
		if err != nil {
			return "", err
		}

		// flip result
		result := []byte{result[1], result[0], result[3], result[2]}

		midBigUint32, err := resultToString(result, binary.BigEndian, "uint32")
		if err != nil {
			return "", err
		}
		midBigInt32, err := resultToString(result, binary.BigEndian, "int32")
		if err != nil {
			return "", err
		}
		midBigFloat32, err := resultToString(result, binary.BigEndian, "float32")
		if err != nil {
			return "", err
		}
		midLittleUint32, err := resultToString(result, binary.LittleEndian, "uint32")
		if err != nil {
			return "", err
		}
		midLittleInt32, err := resultToString(result, binary.LittleEndian, "int32")
		if err != nil {
			return "", err
		}
		midLittleFloat32, err := resultToString(result, binary.LittleEndian, "float32")
		if err != nil {
			return "", err
		}

		return strings.Join([]string{
			fmt.Sprintf("INT32  - Big Endian (ABCD):    %s", bigInt32),
			fmt.Sprintf("INT32  - Little Endian (DCBA): %s", littleInt32),
			fmt.Sprintf("INT32  - Mid-Big Endian (BADC):    %s", midBigInt32),
			fmt.Sprintf("INT32  - Mid-Little Endian (CDAB): %s", midLittleInt32),
			"",
			fmt.Sprintf("UINT32 - Big Endian (ABCD):    %s", bigUint32),
			fmt.Sprintf("UINT32 - Little Endian (DCBA): %s", littleUint32),
			fmt.Sprintf("UINT32 - Mid-Big Endian (BADC):    %s", midBigUint32),
			fmt.Sprintf("UINT32 - Mid-Little Endian (CDAB): %s", midLittleUint32),
			"",
			fmt.Sprintf("Float32 - Big Endian (ABCD):    %s", bigFloat32),
			fmt.Sprintf("Float32 - Little Endian (DCBA): %s", littleFloat32),
			fmt.Sprintf("Float32 - Mid-Big Endian (BADC):    %s", midBigFloat32),
			fmt.Sprintf("Float32 - Mid-Little Endian (CDAB): %s", midLittleFloat32),
		}, "\n"), nil

	default:
		return "", fmt.Errorf("can't convert data with length %d", len(result))
	}
}

func resultToString(r []byte, order binary.ByteOrder, varType string) (string, error) {
	switch varType {
	case "string":
		return string(r), nil
	case "uint16":
		return fmt.Sprintf("%d", order.Uint16(r)), nil
	case "int16":
		var data int16
		if err := binary.Read(bytes.NewReader(r), order, &data); err != nil {
			return "", err
		}
		return fmt.Sprintf("%d", data), nil
	case "uint32":
		return fmt.Sprintf("%d", order.Uint32(r)), nil
	case "int32":
		var data int32
		if err := binary.Read(bytes.NewReader(r), order, &data); err != nil {
			return "", err
		}
		return fmt.Sprintf("%d", data), nil
	case "uint64":
		return fmt.Sprintf("%d", order.Uint64(r)), nil
	case "int64":
		var data int64
		if err := binary.Read(bytes.NewReader(r), order, &data); err != nil {
			return "", err
		}
		return fmt.Sprintf("%d", data), nil
	case "float32":
		var data float32
		if err := binary.Read(bytes.NewReader(r), order, &data); err != nil {
			return "", err
		}
		return fmt.Sprintf("%f", data), nil
	}
	return "", fmt.Errorf("unsupported datatype: %s", varType)
}

type logger interface {
	Printf(format string, v ...interface{})
}

type option struct {
	address string
	slaveID int
	timeout time.Duration

	logger logger

	rtu struct {
		baudrate int
		dataBits int
		parity   string
		stopBits int
		rs485    struct {
			enabled            bool
			delayRtsBeforeSend time.Duration
			delayRtsAfterSend  time.Duration
			rtsHighDuringSend  bool
			rtsHighAfterSend   bool
			rxDuringTx         bool
		}
	}

	tcp struct {
		linkRecoveryTimeout     time.Duration
		protocolRecoveryTimeout time.Duration
	}
}

func newHandler(o option) (modbus.ClientHandler, error) {
	u, err := url.Parse(o.address)
	if err != nil {
		return nil, err
	}
	switch u.Scheme {
	case "rtu":
		h := modbus.NewRTUClientHandler(u.Path)
		h.Timeout = o.timeout
		h.SlaveID = byte(o.slaveID)
		h.Logger = o.logger
		h.BaudRate = o.rtu.baudrate
		h.DataBits = o.rtu.dataBits
		h.Parity = o.rtu.parity
		h.StopBits = o.rtu.stopBits
		h.RS485 = serial.RS485Config{
			Enabled:            o.rtu.rs485.enabled,
			DelayRtsBeforeSend: o.rtu.rs485.delayRtsBeforeSend,
			DelayRtsAfterSend:  o.rtu.rs485.delayRtsAfterSend,
			RtsHighDuringSend:  o.rtu.rs485.rtsHighDuringSend,
			RtsHighAfterSend:   o.rtu.rs485.rtsHighAfterSend,
			RxDuringTx:         o.rtu.rs485.rxDuringTx,
		}
		return h, nil
	case "tcp":
		h := modbus.NewTCPClientHandler(u.Host)
		h.Timeout = o.timeout
		h.SlaveID = byte(o.slaveID)
		h.LinkRecoveryTimeout = o.tcp.linkRecoveryTimeout
		h.ProtocolRecoveryTimeout = o.tcp.protocolRecoveryTimeout
		h.Logger = o.logger
		return h, nil
	}
	return nil, fmt.Errorf("unsupported scheme: %s", u.Scheme)
}

type binaryWriter interface {
	PutUint32(b []byte, v uint32)
	PutUint16(b []byte, v uint16)
	PutFloat32(b []byte, v float32)
	PutFloat64(b []byte, v float64)
}

func newWriter(o binary.ByteOrder) *writer {
	return &writer{o}
}

type writer struct {
	binary.ByteOrder
}

func (w *writer) PutFloat32(b []byte, v float32) {
	buf := bytes.NewBuffer(b)
	w.to(buf, v)
}

func (w *writer) PutFloat64(b []byte, v float64) {
	buf := bytes.NewBuffer(b)
	w.to(buf, v)
}

func (w *writer) to(buf io.Writer, f interface{}) {
	if err := binary.Write(buf, w.ByteOrder, f); err != nil {
		panic(fmt.Sprintf("binary.Write failed: %s", err.Error()))
	}
}
