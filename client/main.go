package main

import (
	"BCDns_daemon/message"
	"bufio"
	"context"
	"flag"
	"fmt"
	"google.golang.org/grpc"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	SwapCert int = iota
	Start
	Stop
	TestPerformence
	SwitchMode
)

type Node struct {
	IP string
	Client BCDns_daemon.MethodClient
	IsLeader bool
}

var (
	action = flag.Int("action", 0, "Action")
	ip = flag.String("ip", "", "IP")
	frq = flag.Float64("frq", 25, "frequency ms")
	byzantine = flag.Bool("by", false, "Byzantine")
	mode = flag.Int("Mode", 1, "1:myBft; 2:pbft")
	stagger = flag.Bool("stagger", false, "stagger start server")
	test = flag.String("test", "no", "test mode yes/no; default no")
	delay = flag.Int("delay", 0, "Set network delay default 0")
	hosts = map[string]Node{}
	Leader *Node
)

func main() {
	flag.Parse()
	file, err := os.Open("/tmp/hosts")
	defer file.Close()
	if err != nil {
		panic(err)
	}

	bf := bufio.NewReader(file)
	for {
		line, _, err := bf.ReadLine()
		if err == io.EOF {
			break
		}
		l := string(line)
		node := Node{
			IP:strings.Split(l, " ")[0] + ":5000",
		}
		conn, err := grpc.Dial(node.IP, grpc.WithInsecure())
		if err != nil {
			panic(hosts[strings.Split(l, " ")[1]])
		}
		defer conn.Close()

		node.Client = BCDns_daemon.NewMethodClient(conn)
		hosts[strings.Split(l, " ")[1]] = node
	}
	switch *action {
	case SwapCert:
		for _, node := range hosts {
			_, err = node.Client.DoSwapCert(context.Background(), &BCDns_daemon.SwapCertMsg{
				Ip: *ip,
			})
			if err != nil {
				fmt.Println(err, node)
			}
		}
	case Start:
		count := int32(0)
		wt := &sync.WaitGroup{}
		f := (len(hosts) - 1) / 3
		index := 0
		for _, node := range hosts {
			wt.Add(1)
			go func(node Node, b bool) {
				defer wt.Done()
				fmt.Println(b, node)
				var req BCDns_daemon.StartServerReq
				req.Byzantine = b
				req.Test = *test
				req.Delay = int32(*delay)
				switch *mode {
				case 1: req.Mode = "MYBFT"
				case 2: req.Mode = "PBFT"
				}
				rep, err := node.Client.DoStartServer(context.Background(), &req)
				if err != nil {
					fmt.Println(err, node)
					return
				}
				if rep.IsLeader {
					Leader = &node
				}
				atomic.AddInt32(&count, 1)
			}(node, *byzantine && index < f)
			index++
			if *stagger {
				time.Sleep(time.Millisecond * 300)
			}
		}
		wt.Wait()
		fmt.Println("Leader is", Leader)
		if count == int32(len(hosts)) {
			rep, err := Leader.Client.DoStartClient(context.Background(), &BCDns_daemon.StartClientReq{
				Frq: float32(*frq),
			})
			if err != nil {
				fmt.Println(err)
			}
			fmt.Println(rep.Latency, rep.Throughout, rep.SendRate)
		}
		for _, node := range hosts {
			wt.Add(1)
			go func(node Node) {
				defer wt.Done()
				_, err = node.Client.DoStop(context.Background(), &BCDns_daemon.StopMsg{})
				if err != nil {
					fmt.Println(err, node)
				}
			}(node)
		}
		wt.Wait()
	case Stop:
		wt := &sync.WaitGroup{}
		for _, node := range hosts {
			wt.Add(1)
			go func(node Node) {
				defer wt.Done()
				_, err = node.Client.DoStop(context.Background(), &BCDns_daemon.StopMsg{})
				if err != nil {
					fmt.Println(err, node)
				}
			}(node)
		}
		wt.Wait()
	case TestPerformence:
		var amount, s int32
		wt := &sync.WaitGroup{}
		for _, node := range hosts {
			wt.Add(1)
			go func(node Node) {
				defer wt.Done()
				rep, err := node.Client.DoTest(context.Background(), &BCDns_daemon.TestReq{})
				if err != nil {
					fmt.Println(err, node)
					return
				}
				atomic.AddInt32(&amount, rep.Count)
				atomic.AddInt32(&s, 1)
			}(node)
		}
		wt.Wait()
		fmt.Println(amount / s)
	case SwitchMode:
		for _, node := range hosts {
			_, err = node.Client.DoSwitchMode(context.Background(), &BCDns_daemon.SwitchReq{
				Mode: int32(*mode),
			})
			if err != nil {
				fmt.Println(err, node)
			}
		}
	}
}
