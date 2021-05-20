package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"strings"
	"time"

	pb "github.com/ucloud/ularm-monitor-subscribe-demo/proto/metric"

	"google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"
)

var (
	port = flag.Int("port", 10000, "The server port")
)

func main() {
	flag.Parse()

	// 启动 grpc 监听服务
	addr := fmt.Sprintf(":%d", *port)
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	grpcServer := grpc.NewServer(
		grpc.KeepaliveParams(
			keepalive.ServerParameters{
				Time:    3,
				Timeout: 30 * time.Second,
			}),
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			MinTime:             5 * time.Minute,
			PermitWithoutStream: true,
		}),
	)

	pb.RegisterMonitorServer(grpcServer, newServer())

	log.Printf("grpc server listen on [%s]", addr)
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("grpc server start error: %v", err)
	}
}

// proto MonitorServer 接口实现结构体
type sdnboxMonitorServer struct{}

func newServer() *sdnboxMonitorServer {
	s := &sdnboxMonitorServer{}
	return s
}

// Establish proto 接口方法实现
// 接收客户端请求，建立stream长链接，并处理数据回传
func (s *sdnboxMonitorServer) Establish(stream pb.Monitor_EstablishServer) error {
	exitCh := make(chan struct{})

	log.Println("server Establish ...")

	// 第一次建立连接时接收过滤信息
	in, err := stream.Recv()
	if err == io.EOF {
		return nil
	}

	if err != nil {
		log.Printf("stream recv error: %s", err.Error())
		return err
	}

	// 检测请求参数
	err = checkFetchParam(in.AppId, in.MetricName)
	if err != nil {
		log.Printf("check fetch data param error: %s, regData: %+v", err.Error(), in)
		return err
	}

	// 监测 客户端 stream 连接是否断开
	go monitorCliConn(stream, exitCh)

	// 数据回传
	go sendMonitorData(stream, in, exitCh)

	<-exitCh

	log.Printf("grpc stream closed, exit stream, appId: %s, metricName: %s", in.AppId, in.MetricName)

	return nil
}

// monitorCliConn 监测 客户端 stream 连接是否断开
func monitorCliConn(stream pb.Monitor_EstablishServer, exitCh chan struct{}) {
	for {
		_, err := stream.Recv()
		if err == io.EOF {
			log.Println("grpc conn recv done, io EOF")

			close(exitCh)
			return
		}

		if err != nil {
			log.Printf("grpc conn recv error: %s", err.Error())

			close(exitCh)
			return
		}
	}
}

// sendMonitorData 数据回传
func sendMonitorData(stream pb.Monitor_EstablishServer, reqData *pb.EstablishRequest, exitCh chan struct{}) {
	// 模拟数据回传，每3s发送一条数据
	for {
		select {
		case _, isOpen := <-exitCh:
			if !isOpen {
				log.Printf("stream[%s] is closed, exit", reqData.AppId)
				return
			}
		case <-time.After(3 * time.Second):
			data := genData(reqData)
			if err := stream.Send(data); err != nil {
				log.Printf("send data to conn error: %s, data: %v", err.Error(), data)

				break
			}

			log.Printf("send data to conn successfully, data: %v", data)
		}
	}
}

func checkFetchParam(appId, metricName string) error {
	if (strings.TrimSpace(appId) == "") || (strings.TrimSpace(metricName) == "") {
		return errors.New("AppId or MetricName empty")
	}

	return nil
}

func genData(reqData *pb.EstablishRequest) *pb.EstablishResponse {
	now := time.Now().Unix()

	res := &pb.EstablishResponse{
		AppId:            reqData.AppId,
		Timestamp:        now,
		ReceiveTimestamp: now,
		MetricName:       reqData.MetricName,
		MetricValue:      fmt.Sprintf("%d", now),
		MetricTags:       reqData.TagFilters,
	}

	return res
}
