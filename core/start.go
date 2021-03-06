package core

import (
	"bufio"
	"context"
	"fmt"
	"github.com/google/gopacket/pcap"
	ratelimit "golang.org/x/time/rate"
	"io"
	"math/rand"
	"os"
	"strings"
	"sync/atomic"
	"time"
)

func Start(options *Options) {
	version := pcap.Version()
	fmt.Println(version)
	ether := GetDevices(options.NetworkId)
	LocalStack = NewStack()
	fmt.Println("启动接收模块,设置rate:", options.Rate, "pps")
	fmt.Println("DNS:", options.Resolvers)
	// 设定接收的ID
	flagID := uint16(RandInt64(400, 654))
	go Recv(ether.Device, options, flagID)
	sendog := SendDog{}
	sendog.Init(ether, options.Resolvers, flagID)

	var f io.Reader
	if options.Stdin {
		f = os.Stdin
	} else if options.Domain != "" {
		if options.FileName == "" {
			fmt.Println("加载内置字典")
			f = strings.NewReader(DefaultSubdomain)
		} else {
			f2, err := os.Open(options.FileName)
			defer f2.Close()
			if err != nil {
				panic(err)
			}
			f = f2
		}
	} else if options.Verify {
		f2, err := os.Open(options.FileName)
		defer f2.Close()
		if err != nil {
			panic(err)
		}
		f = f2
	}
	r := bufio.NewReader(f)

	limiter := ratelimit.NewLimiter(ratelimit.Every(time.Duration(time.Second.Nanoseconds()/options.Rate)), int(options.Rate))
	ctx := context.Background()
	// 协程重发线程
	stop := make(chan string)
	go func() {
		for {
			// 循环检测超时的队列
			//遍历该map，参数是个函数，该函数参的两个参数是遍历获得的key和value，返回一个bool值，当返回false时，遍历立刻结束。
			LocalStauts.Range(func(k, v interface{}) bool {
				index := k.(uint32)
				value := v.(StatusTable)
				if value.Retry >= 25 {
					atomic.AddUint64(&FaildIndex, 1)
					LocalStauts.Delete(index)
					return true
				}
				if time.Now().Unix()-value.Time >= 5 {
					_ = limiter.Wait(ctx)
					value.Retry++
					value.Time = time.Now().Unix()
					value.Dns = sendog.ChoseDns()
					LocalStauts.Store(index, value)
					flag2, srcport := GenerateFlagIndexFromMap(index)
					sendog.Send(value.Domain, value.Dns, srcport, flag2)
				}
				time.Sleep(time.Microsecond * time.Duration(rand.Intn(300)+100))
				return true
			})
		}
	}()
	go func() {
		t := time.NewTicker(time.Millisecond * 300)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				fmt.Printf("\rSuccess:%d Sent:%d Recved:%d Faild:%d", SuccessIndex, SentIndex, RecvIndex, FaildIndex)
			case <-stop:
				return
			}
		}
	}()
	for {
		_ = limiter.Wait(ctx)
		line, _, err := r.ReadLine()
		if err != nil {
			break
		}
		msg := string(line)
		if msg == "" {
			continue
		}
		var _domain string
		if options.Verify || options.Stdin {
			_domain = msg
		} else {
			_domain = msg + "." + options.Domain
		}
		dnsname := sendog.ChoseDns()
		flagid2, scrport := sendog.BuildStatusTable(_domain, dnsname)
		sendog.Send(_domain, dnsname, scrport, flagid2)
	}
	for {
		var isbreak bool = true
		LocalStauts.Range(func(k, v interface{}) bool {
			isbreak = false
			return false
		})
		if isbreak {
			stop <- "i love u,lxk"
			break
		}
		time.Sleep(time.Second * 1)
	}
	for i := 5; i >= 0; i-- {
		fmt.Printf("检测完毕，等待%ds\n", i)
		time.Sleep(time.Second * 1)
	}
	sendog.Close()
}
