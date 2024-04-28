package main

import (
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/url"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
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
		register        = flag.Int("register", -1, "")
		fnCode          = flag.Int("fn-code", 0x03, "fn")
		quantity        = flag.Int("quantity", 2, "register quantity, length in bytes")
		ignoreCRCError  = flag.Bool("ignore-crc", false, "ignore crc")
		eType           = flag.String("type-exec", "uint16", "")
		pType           = flag.String("type-parse", "raw", "type to parse the register result. Use 'raw' if you want to see the raw bits and bytes. Use 'all' if you want to decode the result to different commonly used formats.")
		writeValue      = flag.Float64("write-value", math.MaxFloat64, "")
		readParseOrder  = flag.String("read-parse-order", "", "order to parse the register that was read out. Valid values: [AB, BA, ABCD, DCBA, BADC, CDAB]. Can only be used for 16bit (1 register) and 32bit (2 registers). If used, it will overwrite the big-endian or little-endian parameter.")
		writeParseOrder = flag.String("write-exec-order", "", "order to execute the register(s) that should be written to. Valid values: [AB, BA, ABCD, DCBA, BADC, CDAB]. Can only be used for 16bit (1 register) and 32bit (2 registers). If used, it will overwrite the big-endian or little-endian parameter.")
		parseBigEndian  = flag.Bool("order-parse-bigendian", true, "t: big, f: little")
		execBigEndian   = flag.Bool("order-exec-bigendian", true, "t: big, f: little")
		filename        = flag.String("filename", "", "")
		logframe        = flag.Bool("log-frame", false, "prints received and send modbus frame to stdout")
	)

	flag.Parse()

	if len(os.Args) == 1 {
		flag.PrintDefaults()
		return
	}

	logger := slog.Default()
	if *register > math.MaxUint16 || *register < 0 {
		intRegister := *register
		logger.Error("invalid register value: " + strconv.Itoa(intRegister))
		os.Exit(-1)
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
		logger.Error(err.Error())
		os.Exit(-1)
	}
	if err := handler.Connect(); err != nil {
		logger.Error(err.Error())
		os.Exit(-1)
	}
	defer handler.Close()

	client := modbus.NewClient(handler)

	result, err := exec(client, eo, *writeParseOrder, *register, *fnCode, *writeValue, *eType, *quantity)
	if err != nil && strings.Contains(err.Error(), "crc") && *ignoreCRCError {
		logger.Info("ignoring crc error: %+v\n", err)
	} else if err != nil {
		logger.Error(err.Error())
		os.Exit(-1)
	}

	var res string
	switch *pType {
	case "raw":
		res, err = resultToRawString(result, int(startReg))
	case "all":
		res, err = resultToAllString(result)
	default:
		res, err = resultToString(result, po, *readParseOrder, *pType)
	}

	if err != nil {
		logger.Error(err.Error())
		os.Exit(-1)
	}

	logger.Info(res)

	if *filename != "" {
		if err := resultToFile([]byte(res), *filename); err != nil {
			logger.Error(err.Error())
			os.Exit(-1)
		}
		fName := *filename
		logger.Info(fName + " successfully written\n")
	}
}

func exec(
	client modbus.Client,
	o binary.ByteOrder,
	forcedOrder string,
	register int,
	fnCode int,
	wval float64,
	etype string,
	quantity int,
) ([]byte, error) {
	var err error
	var result []byte
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
			break
		}
		result, err = client.WriteSingleRegister(uint16(register), uint16(wval))
	case 0x10:
		var buf []byte
		buf, err = convertToBytes(etype, o, forcedOrder, wval)
		if err != nil {
			break
		}
		result, err = client.WriteMultipleRegisters(uint16(register), uint16(len(buf))/2, buf)
	case 0x04:
		result, err = client.ReadInputRegisters(uint16(register), uint16(quantity))
	case 0x03:
		result, err = client.ReadHoldingRegisters(uint16(register), uint16(quantity))
	default:
		err = fmt.Errorf("function code %d is unsupported", fnCode)
	}
	return result, err
}

func convertToBytes(eType string, order binary.ByteOrder, forcedOrder string, val float64) ([]byte, error) {
	fo := strings.ToUpper(forcedOrder)
	switch fo {
	case "":
		// nothing is forced
	case "AB", "ABCD", "BADC":
		order = binary.BigEndian
	case "BA", "DCBA", "CDAB":
		order = binary.LittleEndian
	default:
		return nil, fmt.Errorf("forced order %s not known", strings.ToUpper(forcedOrder))
	}

	w := newWriter(order)
	var buf []byte
	var err error
	switch eType {
	case "uint16":
		max := float64(math.MaxUint16)
		if val > max || val < 0 {
			err = fmt.Errorf("overflow: %f does not fit into datatype %s", val, eType)
			break
		}
		buf = w.ToUint16(uint16(val))
	case "uint32":
		max := float64(math.MaxUint32)
		if val > max || val < 0 {
			err = fmt.Errorf("overflow: %f does not fit into datatype %s", val, eType)
			break
		}
		buf = w.ToUint32(uint32(val))
	case "float32":
		max := float64(math.MaxFloat32)
		min := -float64(math.MaxFloat32)
		if val > max || val < min {
			err = fmt.Errorf("overflow: %f does not fit into datatype %s", val, eType)
			break
		}
		buf = w.ToFloat32(float32(val))
	case "float64":
		buf = w.ToFloat64(float64(val))
	}

	// flip bytes when CDAB or BADC are used (and we have 4 bytes)
	if fo == "CDAB" || fo == "BADC" && len(buf) == 4 {
		buf = []byte{buf[1], buf[0], buf[3], buf[2]}
	}

	return buf, err
}

func resultToFile(r []byte, filename string) error {
	return os.WriteFile(filename, r, 0644)
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
	buf := new(bytes.Buffer)
	w := tabwriter.NewWriter(buf, 0, 0, 2, ' ', 0)

	switch len(result) {
	case 2:
		bigUint16, err := resultToString(result, binary.BigEndian, "", "uint16")
		if err != nil {
			return "", err
		}
		bigInt16, err := resultToString(result, binary.BigEndian, "", "int16")
		if err != nil {
			return "", err
		}
		littleUint16, err := resultToString(result, binary.LittleEndian, "", "uint16")
		if err != nil {
			return "", err
		}
		littleInt16, err := resultToString(result, binary.LittleEndian, "", "int16")
		if err != nil {
			return "", err
		}

		fmt.Fprintf(w, "INT16\tBig Endian (AB):\t%s\t\n", bigInt16)
		fmt.Fprintf(w, "INT16\tLittle Endian (BA):\t%s\t\n", littleInt16)
		fmt.Fprintln(w, "\t")
		fmt.Fprintf(w, "UINT16\tBig Endian (AB):\t%s\t\n", bigUint16)
		fmt.Fprintf(w, "UINT16\tLittle Endian (BA):\t%s\t\n", littleUint16)

		err = w.Flush()
		if err != nil {
			return "", err
		}

		return buf.String(), nil
	case 4:
		bigUint32, err := resultToString(result, binary.BigEndian, "", "uint32")
		if err != nil {
			return "", err
		}
		bigInt32, err := resultToString(result, binary.BigEndian, "", "int32")
		if err != nil {
			return "", err
		}
		bigFloat32, err := resultToString(result, binary.BigEndian, "", "float32")
		if err != nil {
			return "", err
		}
		littleUint32, err := resultToString(result, binary.LittleEndian, "", "uint32")
		if err != nil {
			return "", err
		}
		littleInt32, err := resultToString(result, binary.LittleEndian, "", "int32")
		if err != nil {
			return "", err
		}
		littleFloat32, err := resultToString(result, binary.LittleEndian, "", "float32")
		if err != nil {
			return "", err
		}

		midBigUint32, err := resultToString(result, binary.BigEndian, "BADC", "uint32")
		if err != nil {
			return "", err
		}
		midBigInt32, err := resultToString(result, binary.BigEndian, "BADC", "int32")
		if err != nil {
			return "", err
		}
		midBigFloat32, err := resultToString(result, binary.BigEndian, "BADC", "float32")
		if err != nil {
			return "", err
		}
		midLittleUint32, err := resultToString(result, binary.LittleEndian, "CDAB", "uint32")
		if err != nil {
			return "", err
		}
		midLittleInt32, err := resultToString(result, binary.LittleEndian, "CDAB", "int32")
		if err != nil {
			return "", err
		}
		midLittleFloat32, err := resultToString(result, binary.LittleEndian, "CDAB", "float32")
		if err != nil {
			return "", err
		}

		fmt.Fprintf(w, "INT32\tBig Endian (ABCD):\t%s\t\n", bigInt32)
		fmt.Fprintf(w, "INT32\tLittle Endian (DCBA):\t%s\t\n", littleInt32)
		fmt.Fprintf(w, "INT32\tMid-Big Endian (BADC):\t%s\t\n", midBigInt32)
		fmt.Fprintf(w, "INT32\tMid-Little Endian (CDAB):\t%s\t\n", midLittleInt32)
		fmt.Fprintln(w, "\t")
		fmt.Fprintf(w, "UINT32\tBig Endian (ABCD):\t%s\t\n", bigUint32)
		fmt.Fprintf(w, "UINT32\tLittle Endian (DCBA):\t%s\t\n", littleUint32)
		fmt.Fprintf(w, "UINT32\tMid-Big Endian (BADC):\t%s\t\n", midBigUint32)
		fmt.Fprintf(w, "UINT32\tMid-Little Endian (CDAB):\t%s\t\n", midLittleUint32)
		fmt.Fprintln(w, "\t")
		fmt.Fprintf(w, "Float32\tBig Endian (ABCD):\t%s\t\n", bigFloat32)
		fmt.Fprintf(w, "Float32\tLittle Endian (DCBA):\t%s\t\n", littleFloat32)
		fmt.Fprintf(w, "Float32\tMid-Big Endian (BADC):\t%s\t\n", midBigFloat32)
		fmt.Fprintf(w, "Float32\tMid-Little Endian (CDAB):\t%s\t\n", midLittleFloat32)
		err = w.Flush()
		if err != nil {
			return "", err
		}

		return buf.String(), nil
	default:
		return "", fmt.Errorf("can't convert data with length %d", len(result))
	}
}

func resultToString(r []byte, order binary.ByteOrder, forcedOrder string, varType string) (string, error) {
	fo := strings.ToUpper(forcedOrder)
	switch fo {
	case "":
		// nothing is forced
	case "AB", "ABCD", "BADC":
		order = binary.BigEndian
	case "BA", "DCBA", "CDAB":
		order = binary.LittleEndian
	default:
		return "", fmt.Errorf("forced order %s not known", strings.ToUpper(forcedOrder))
	}

	if fo == "CDAB" || fo == "BADC" && len(r) == 4 {
		// flip result
		r = []byte{r[1], r[0], r[3], r[2]}
	}

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

type option struct {
	address string
	slaveID int
	timeout time.Duration

	logger *slog.Logger

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
	case "udp":
		h := modbus.NewRTUOverUDPClientHandler(u.Host)
		h.SlaveID = byte(o.slaveID)
		h.Logger = o.logger
		return h, nil
	}

	return nil, fmt.Errorf("unsupported scheme: %s", u.Scheme)
}

func newWriter(o binary.ByteOrder) *writer {
	return &writer{order: o}
}

type writer struct {
	order binary.ByteOrder
}

func (w *writer) ToUint16(v uint16) []byte {
	var buf bytes.Buffer
	w.to(&buf, v)
	b, _ := io.ReadAll(&buf)
	return b
}

func (w *writer) ToUint32(v uint32) []byte {
	var buf bytes.Buffer
	w.to(&buf, v)
	b, _ := io.ReadAll(&buf)
	return b
}

func (w *writer) ToFloat32(v float32) []byte {
	var buf bytes.Buffer
	w.to(&buf, v)
	b, _ := io.ReadAll(&buf)
	return b
}

func (w *writer) ToFloat64(v float64) []byte {
	var buf bytes.Buffer
	w.to(&buf, v)
	b, _ := io.ReadAll(&buf)
	return b
}

func (w *writer) to(buf io.Writer, f interface{}) {
	if err := binary.Write(buf, w.order, f); err != nil {
		panic(fmt.Sprintf("binary.Write failed: %s", err.Error()))
	}
}
