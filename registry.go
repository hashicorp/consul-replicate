package main

import (
	"github.com/hashicorp/errwrap"
	"github.com/hashicorp/consul/api"
	"fmt"
	"log"
	"net"
	"io"
	"net/http"
	"encoding/json"
	"os"
)

var (
	defaultLocalIp string
	localIP        string
	healthPort     string
)

const (
	defaultConsulTimeout  = "5s"
	defaultConsulInterval = "10s"
	serviceId             = "consul-replicate"
	serviceName           = "consul-replicate"
)

func init() {
	addrs, err := net.InterfaceAddrs()

	if err != nil {
		log.Printf("init local ip error\n")
		localIP = defaultLocalIp
		return
	}

	for _, address := range addrs {

		// 检查ip地址判断是否回环地址
		if ipnet, ok := address.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				localIP = ipnet.IP.String()
				break
			}
		}
	}
	var ok bool
	healthPort, ok = os.LookupEnv("CONSUL_REPLICATE_HEALTH_PORT")
	if !ok {
		healthPort = "9000"
	}
}
func (r *Runner) RunServiceDiscovery() (error error) {
	go r.serveHealth()
	service := &api.AgentServiceRegistration{
		ID:                serviceId,
		Name:              serviceName,
		Address:           localIP,
		EnableTagOverride: false,
	}
	client := r.clients.Consul()
	agent := client.Agent()
	if err := agent.ServiceRegister(service); err != nil {
		return errwrap.Wrapf(`service registration failed: {{err}}`, err)
	}
	check := new(api.AgentServiceCheck)
	check.HTTP = fmt.Sprintf("http://%s:%d%s", localIP, healthPort, "/health")
	check.Timeout = defaultConsulTimeout
	check.Interval = defaultConsulInterval

	service.Check = check
	err := client.Agent().ServiceRegister(service)
	if err != nil {
		log.Fatalf("Register error,err:%s", err)
		return err
	} else {
		log.Println("Register success")
	}

	return nil
}

func (r *Runner) Dregister() error {
	log.Println("begin to Dregister")

	client := r.clients.Consul()
	err := client.Agent().ServiceDeregister(serviceId)
	if err != nil {
		log.Fatalf("Deregister error: %s", err)
		return err
	} else {
		log.Println("Dregister success")
	}

	return nil
}

func (r *Runner) serveHealth() error {
	http.HandleFunc("/health", func(w http.ResponseWriter, req *http.Request) { io.WriteString(w, r.Health()) })
	log.Printf("HTTP Port: %s", healthPort)
	err := http.ListenAndServe(":"+healthPort, nil)
	if err != nil {
		log.Fatalf("[health] ListenAndServe: %s", err)
		return err
	}
	return nil
}

func (r *Runner) Health() string {

	client := r.clients.Consul()
	services, _ := client.Agent().Services()
	statusInfo := make(map[string]interface{})
	statusInfo["services"] = services
	statusInfo["status"] = "UP"
	ret, _ := json.Marshal(statusInfo)
	return string(ret)
}
