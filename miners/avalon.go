// Package miners provides functionality for discovering and controlling cryptocurrency miners.
package miners

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"iter"
	"net"
	"net/netip"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Sender is a function type for sending data over a network connection.
type Sender func(conn net.Conn) error

// Receiver is a generic function type for receiving data over a network connection.
type Receiver[T any] func(conn net.Conn) (T, error)

// UnmarshalJSON implements custom JSON unmarshaling for StatsItem.
func (s *StatsItem) UnmarshalJSON(data []byte) error {
	var raw map[string]string
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	mmidSummary, ok := raw["MM ID0:Summary"]
	if !ok {
		return fmt.Errorf("MM ID0:Summary field not found")
	}

	if mmidSummary == "" {
		return fmt.Errorf("MM ID0:Summary is empty")
	}

	stats := &AvalonLiteStats{}

	// Regular expression to match Key[Value] patterns
	re := regexp.MustCompile(`([A-Za-z0-9\s]+)\[([^\]]*)\]`)
	matches := re.FindAllStringSubmatch(mmidSummary, -1)

	for _, match := range matches {
		if len(match) != 3 {
			continue
		}

		key := strings.TrimSpace(match[1])
		value := match[2]

		switch key {
		case "Ver":
			stats.Ver = value
		case "LVer":
			stats.LVer = value
		case "BVer":
			stats.BVer = value
		case "HashMcu0Ver":
			stats.HashMcu0Ver = value
		case "FanMcuVer":
			stats.FanMcuVer = value
		case "CPU":
			stats.CPU = value
		case "FW":
			stats.FW = value
		case "DNA":
			stats.DNA = value
		case "STATE":
			if i, err := strconv.Atoi(value); err == nil {
				stats.State = AvalonState(i)
			}
		case "MEMFREE":
			if i, err := strconv.Atoi(value); err == nil {
				stats.MemFree = i
			}
		case "NETFAIL":
			timestamps := strings.Fields(value)
			stats.NetFail = make([]int64, 0, len(timestamps))
			for _, ts := range timestamps {
				if i, err := strconv.ParseInt(ts, 10, 64); err == nil {
					stats.NetFail = append(stats.NetFail, i)
				}
			}
		case "SSID":
			stats.SSID = value
		case "RSSI":
			if i, err := strconv.Atoi(value); err == nil {
				stats.RSSI = i
			}
		case "NetDevType":
			if i, err := strconv.Atoi(value); err == nil {
				stats.NetDevType = i
			}
		case "SYSTEMSTATU":
			stats.SystemStatus = value
		case "Elapsed":
			if i, err := strconv.ParseInt(value, 10, 64); err == nil {
				stats.Elapsed = i
			}
		case "BOOTBY":
			stats.BootBy = value
		case "LW":
			if i, err := strconv.ParseInt(value, 10, 64); err == nil {
				stats.LW = i
			}
		case "MH":
			if i, err := strconv.Atoi(value); err == nil {
				stats.MH = i
			}
		case "DHW":
			if i, err := strconv.Atoi(value); err == nil {
				stats.DHW = i
			}
		case "HW":
			if i, err := strconv.Atoi(value); err == nil {
				stats.HW = i
			}
		case "DH":
			stats.DH = value
		case "ITemp":
			if i, err := strconv.Atoi(value); err == nil {
				stats.ITemp = i
			}
		case "HBITemp":
			if i, err := strconv.Atoi(value); err == nil {
				stats.HBITemp = i
			}
		case "HBOTemp":
			if i, err := strconv.Atoi(value); err == nil {
				stats.HBOTemp = i
			}
		case "TMax":
			if i, err := strconv.Atoi(value); err == nil {
				stats.TMax = i
			}
		case "TAvg":
			if i, err := strconv.Atoi(value); err == nil {
				stats.TAvg = i
			}
		case "TarT":
			if i, err := strconv.Atoi(value); err == nil {
				stats.TarT = i
			}
		case "Fan1":
			if i, err := strconv.Atoi(value); err == nil {
				stats.Fan1 = i
			}
		case "Fan2":
			if i, err := strconv.Atoi(value); err == nil {
				stats.Fan2 = i
			}
		case "Fan3":
			if i, err := strconv.Atoi(value); err == nil {
				stats.Fan3 = i
			}
		case "Fan4":
			if i, err := strconv.Atoi(value); err == nil {
				stats.Fan4 = i
			}
		case "FanR":
			// Parse FanR as integer from percentage string (e.g., "71%" -> 71)
			if strings.HasSuffix(value, "%") {
				fanRStr := strings.TrimSuffix(value, "%")
				if i, err := strconv.Atoi(fanRStr); err == nil {
					stats.FanR = i
				}
			} else if i, err := strconv.Atoi(value); err == nil {
				stats.FanR = i
			}
		case "SoftOffTime":
			if i, err := strconv.ParseInt(value, 10, 64); err == nil {
				stats.SoftOffTime = i
			}
		case "SoftOnTime":
			if i, err := strconv.ParseInt(value, 10, 64); err == nil {
				stats.SoftOnTime = i
			}
		case "Filter":
			if i, err := strconv.Atoi(value); err == nil {
				stats.Filter = i
			}
		case "FanErr":
			if i, err := strconv.Atoi(value); err == nil {
				stats.FanErr = i
			}
		case "SoloAllowed":
			if i, err := strconv.Atoi(value); err == nil {
				stats.SoloAllowed = i
			}
		case "PS":
			numbers := strings.Fields(value)
			stats.PS = make([]int, 0, len(numbers))
			for _, num := range numbers {
				if i, err := strconv.Atoi(num); err == nil {
					stats.PS = append(stats.PS, i)
				}
			}
		case "PCOMM_E":
			if i, err := strconv.Atoi(value); err == nil {
				stats.PCommE = i
			}
		case "GHSspd":
			if f, err := strconv.ParseFloat(value, 64); err == nil {
				stats.GHSspd = f
			}
		case "DHspd":
			stats.DHspd = value
		case "GHSmm":
			if f, err := strconv.ParseFloat(value, 64); err == nil {
				stats.GHSmm = f
			}
		case "GHSavg":
			if f, err := strconv.ParseFloat(value, 64); err == nil {
				stats.GHSavg = f
			}
		case "WU":
			if f, err := strconv.ParseFloat(value, 64); err == nil {
				stats.WU = f
			}
		case "Freq":
			if f, err := strconv.ParseFloat(value, 64); err == nil {
				stats.Freq = f
			}
		case "MGHS":
			if f, err := strconv.ParseFloat(value, 64); err == nil {
				stats.MGHS = f
			}
		case "TA":
			if i, err := strconv.Atoi(value); err == nil {
				stats.TA = i
			}
		case "Core":
			stats.Core = value
		case "BIN":
			if i, err := strconv.Atoi(value); err == nil {
				stats.BIN = i
			}
		case "PING":
			if i, err := strconv.Atoi(value); err == nil {
				stats.PING = i
			}
		case "SoftOFF":
			if i, err := strconv.Atoi(value); err == nil {
				stats.SoftOFF = i
			}
		case "ECHU":
			if i, err := strconv.Atoi(value); err == nil {
				stats.ECHU = i
			}
		case "ECMM":
			if i, err := strconv.Atoi(value); err == nil {
				stats.ECMM = i
			}
		case "PLL0":
			numbers := strings.Fields(value)
			stats.PLL0 = make([]int, 0, len(numbers))
			for _, num := range numbers {
				if i, err := strconv.Atoi(num); err == nil {
					stats.PLL0 = append(stats.PLL0, i)
				}
			}
		case "SF0":
			numbers := strings.Fields(value)
			stats.SF0 = make([]int, 0, len(numbers))
			for _, num := range numbers {
				if i, err := strconv.Atoi(num); err == nil {
					stats.SF0 = append(stats.SF0, i)
				}
			}
		case "CRC":
			if i, err := strconv.Atoi(value); err == nil {
				stats.CRC = i
			}
		case "COMCRC":
			if i, err := strconv.Atoi(value); err == nil {
				stats.COMCRC = i
			}
		case "ATA0":
			stats.ATA0 = value
		case "LcdOnoff":
			if i, err := strconv.Atoi(value); err == nil {
				stats.LcdOnoff = i
			}
		case "Activation":
			if i, err := strconv.Atoi(value); err == nil {
				stats.Activation = i
			}
		case "WORKMODE":
			if i, err := strconv.Atoi(value); err == nil {
				stats.WorkMode = AvalonWorkMode(i)
			}
		case "WORKLEVEL":
			if i, err := strconv.Atoi(value); err == nil {
				stats.WorkLevel = i
			}
		case "MPO":
			if i, err := strconv.Atoi(value); err == nil {
				stats.MPO = i
			}
		case "CALIALL":
			if i, err := strconv.Atoi(value); err == nil {
				stats.CALIALL = i
			}
		case "ADJ":
			if i, err := strconv.Atoi(value); err == nil {
				stats.ADJ = i
			}
		case "Nonce Mask":
			if i, err := strconv.Atoi(value); err == nil {
				stats.NonceMask = i
			}
		}
	}

	s.MMIDSummary = stats
	return nil
}

// Discover searches for Avalon miners on the specified network and returns a list of discovered hosts.
func Discover(ctx context.Context, network string) []*AvalonQHost {
	results := make(chan *AvalonQHost)
	var wg sync.WaitGroup
	queue := make(chan string, 25)

	// Collector goroutine - single writer to hosts slice
	hosts := make([]*AvalonQHost, 0)
	done := make(chan struct{})
	go func() {
		for host := range results {
			hosts = append(hosts, host)
		}
		close(done)
	}()

	for a := range getAddresses(ctx, network) {
		address := a.String()
		queue <- address
		wg.Go(func() {
			if v, err := version(ctx, address, 4028); err == nil {
				results <- &AvalonQHost{
					Address: address,
					Port:    4028,
					Version: v,
				}
			}
			<-queue
		})
	}
	wg.Wait()
	close(results)
	<-done
	return hosts
}

func getAddresses(ctx context.Context, network string) iter.Seq[netip.Addr] {
	return func(yield func(netip.Addr) bool) {
		prefix, _ := netip.ParsePrefix(network)
		next := prefix.Addr().Next()
		for next.IsValid() && prefix.Contains(next.Next()) {
			// Check if context is cancelled before yielding each address
			select {
			case <-ctx.Done():
				return
			default:
			}

			if !yield(next) {
				return
			}
			next = next.Next()
		}
	}
}

// Standby puts the Avalon miner into standby mode.
func (h *AvalonQHost) Standby(ctx context.Context) (string, error) {
	if _, err := h.SetWorkMode(ctx, AvalonEcoMode, true); err != nil {
		return "", err
	}
	return send(ctx, h.Address, h.Port,
		func(conn net.Conn) error {
			_, err := fmt.Fprintf(conn, "ascset|0,softoff,1: %d", time.Now().Unix())
			return err
		},
		readStringResponse,
	)
}

// SetWorkMode sets the work mode of the Avalon miner.
func (h *AvalonQHost) SetWorkMode(ctx context.Context, mode AvalonWorkMode, resetHistory bool) (string, error) {
	if resetHistory {
		h.ResetLiteStats()
	}
	return send(ctx, h.Address, h.Port,
		func(conn net.Conn) error {
			_, err := fmt.Fprintf(conn, "ascset|0,workmode,set,%d", mode)
			return err
		},
		readStringResponse,
	)
}

// WakeUp wakes up the Avalon miner from standby mode.
func (h *AvalonQHost) WakeUp(ctx context.Context) (string, error) {
	h.ResetLiteStats()
	return send(ctx, h.Address, h.Port,
		func(conn net.Conn) error {
			_, err := fmt.Fprintf(conn, "ascset|0,softon,1: %d", time.Now().Unix())
			return err
		},
		readStringResponse,
	)
}

// RefreshLiteStats refreshes the lite statistics for the Avalon miner.
func (h *AvalonQHost) RefreshLiteStats(ctx context.Context, maxHistory int) {
	stats, err := send(ctx, h.Address, h.Port,
		func(conn net.Conn) error {
			return writeCommand("litestats", conn)
		},
		func(conn net.Conn) (*AvalonQLiteStats, error) {
			stats := &AvalonQLiteStats{}
			if err := readJSONResponse(conn, stats); err != nil {
				return nil, err
			}
			return stats, nil
		})
	if stats == nil || stats.Stats == nil || len(stats.Stats) == 0 || stats.Stats[0].MMIDSummary == nil {
		err = fmt.Errorf("invalid stats response for miner %s:%d", h.Address, h.Port)
	}
	if err != nil {
		h.AddLiteStats(nil, err, maxHistory)
		return
	}
	h.AddLiteStats(stats.Stats[0].MMIDSummary, err, maxHistory)
}

func version(ctx context.Context, address string, port int) (*AvalonQVersion, error) {
	return send(ctx, address, port,
		func(conn net.Conn) error {
			return writeCommand("version", conn)
		},
		func(conn net.Conn) (*AvalonQVersion, error) {
			v := &AvalonQVersion{}
			if err := readJSONResponse(conn, v); err != nil {
				return nil, err
			}
			return v, nil
		})
}

func send[T any](ctx context.Context, address string, port int, sender Sender, receiver Receiver[T]) (T, error) {
	d := net.Dialer{
		Timeout: time.Second,
	}
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()

	conn, err := d.DialContext(ctx, "tcp", fmt.Sprintf("%s:%d", address, port))
	if err != nil {
		var zero T
		return zero, err
	}
	defer conn.Close()

	// Set deadline for all operations on this connection
	if err := conn.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
		var zero T
		return zero, err
	}

	if err := sender(conn); err != nil {
		var zero T
		return zero, err
	}

	r, err := receiver(conn)
	if err != nil {
		var zero T
		return zero, err
	}

	return r, nil
}

func writeCommand(cmd string, conn net.Conn) error {
	enc := json.NewEncoder(conn)
	return enc.Encode(&AvalonQCommand{
		Command: cmd,
	})
}

func readStringResponse(conn net.Conn) (string, error) {
	r, err := io.ReadAll(conn)
	if err != nil {
		return "", err
	}
	return string(r), nil
}

func readJSONResponse(conn net.Conn, response any) error {
	dec := json.NewDecoder(conn)
	return dec.Decode(response)
}
