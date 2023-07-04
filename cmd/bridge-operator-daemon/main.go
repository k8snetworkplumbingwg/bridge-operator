package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/k8snetworkplumbingwg/bridge-operator/pkg/server"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog"
)

const logFlushFreqFlagName = "log-flush-frequency"

var logFlushFreq = pflag.Duration(logFlushFreqFlagName, 5*time.Second, "Maximum number of seconds between log flushes")

// KlogWriter serves as a bridge between the standard log package and the glog package.
type KlogWriter struct{}

// Write implements the io.Writer interface.
func (writer KlogWriter) Write(data []byte) (n int, err error) {
	klog.InfoDepth(1, string(data))
	return len(data), nil
}

func initLogs() {
	log.SetOutput(KlogWriter{})
	log.SetFlags(0)
	go wait.Forever(klog.Flush, *logFlushFreq)
}

func main() {
	initLogs()
	defer klog.Flush()
	opts := server.NewOptions()

	cmd := &cobra.Command{
		Use:  "bridge-operator-daemon",
		Long: `TBD`,
		Run: func(cmd *cobra.Command, args []string) {
			if err := opts.Run(); err != nil {
				klog.Exit(err)
			}
		},
	}
	opts.AddFlags(cmd.Flags())

	signalCh := make(chan os.Signal, 16)
	signal.Notify(signalCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		for sig := range signalCh {
			klog.Infof("Caught %v, stopping...", sig)
			opts.Stop()
		}
	}()

	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
