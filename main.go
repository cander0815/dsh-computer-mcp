package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"time"
)

func main() {
	home, err := os.UserConfigDir()
	if err != nil {
		log.Fatal(err)
	}
	configPath := flag.String("config", filepath.Join(home, "DSHComputerAgent", "config.json"), "配置文件路径")
	listen := flag.String("listen", "0.0.0.0:18790", "电脑工具连接地址")
	adminListen := flag.String("admin", "127.0.0.1:18791", "本地配置页地址，仅允许 127.0.0.1")
	noBrowser := flag.Bool("no-browser", false, "不自动打开本地配置页")
	prepareConfig := flag.Bool("prepare-config", false, "仅准备配置文件，供 sudo 启动脚本使用")
	executable, err := os.Executable()
	if err != nil {
		log.Fatal(err)
	}
	logDir := flag.String("log-dir", filepath.Dir(executable), "每日操作日志目录，默认与程序同目录")
	flag.Parse()
	if *prepareConfig {
		if _, err := os.Stat(*configPath); os.IsNotExist(err) {
			if _, err := loadConfig(*configPath); err != nil {
				log.Fatal(err)
			}
		} else if err != nil {
			log.Fatal(err)
		}
		return
	}
	host, _, err := net.SplitHostPort(*adminListen)
	if err != nil || host != "127.0.0.1" {
		log.Fatal("本地配置页必须绑定 127.0.0.1")
	}
	cfg, err := loadConfig(*configPath)
	if err != nil {
		log.Fatal(err)
	}
	a, err := newAgent(cfg)
	if err != nil {
		log.Fatal(err)
	}
	a.configPath = *configPath
	if err = os.MkdirAll(*logDir, 0700); err != nil {
		log.Fatal(err)
	}
	a.audit = newDailyLogger(*logDir, *configPath)
	a.audit.write(map[string]any{"event": "连接器启动", "version": version, "privilege": privilegeMode()})
	if errText := a.audit.errorText(); errText != "" {
		log.Fatal("无法创建操作日志：", errText)
	}
	ln, err := net.Listen("tcp4", *listen)
	if err != nil {
		log.Fatal(err)
	}
	a.port = ln.Addr().(*net.TCPAddr).Port
	adminLN, err := net.Listen("tcp4", *adminListen)
	if err != nil {
		ln.Close()
		log.Fatal(err)
	}
	baseContext, cancelAll := context.WithCancel(context.Background())
	defer cancelAll()
	mcp := &http.Server{Handler: a.MCPHandler(), BaseContext: func(net.Listener) context.Context { return baseContext }, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 130 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16384}
	admin := &http.Server{Handler: a.AdminHandler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16384}
	fmt.Printf("DSH 电脑连接器 v%s\n本地配置页：http://%s/#%s\n", version, adminLN.Addr().String(), a.adminToken)
	for _, ip := range localAddresses() {
		fmt.Println("工具连接地址：http://" + net.JoinHostPort(ip, strconv.Itoa(a.port)) + "/mcp")
	}
	fmt.Println("当前权限：" + privilegeMode() + "；日志目录：" + *logDir)
	fmt.Println("首次使用请打开本地配置页，选择项目目录。关闭此程序即断开连接。")
	stopped := make(chan os.Signal, 1)
	signal.Notify(stopped, shutdownSignals()...)
	fail := make(chan error, 2)
	go func() { fail <- mcp.Serve(ln) }()
	go func() { fail <- admin.Serve(adminLN) }()
	if !*noBrowser {
		url := "http://" + adminLN.Addr().String() + "/#" + a.adminToken
		go func() {
			if err := browserCommand(url).Run(); err != nil {
				log.Print("自动打开配置页失败，请复制终端显示的完整地址。")
				a.audit.write(map[string]any{"event": "打开配置页失败"})
			}
		}()
	}
	select {
	case <-stopped:
	case err = <-fail:
		if err != http.ErrServerClosed {
			log.Print(err)
		}
	}
	a.audit.write(map[string]any{"event": "连接器停止"})
	cancelAll()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	mcp.Shutdown(ctx)
	admin.Shutdown(ctx)
}
