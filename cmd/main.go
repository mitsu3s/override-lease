package main

import (
	"flag"
	"os"

	overridev1alpha1 "github.com/mitsu3s/override-lease/api/v1alpha1"
	"github.com/mitsu3s/override-lease/internal/controller"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

var setupLog = ctrl.Log.WithName("setup")

func main() {
	var metricsAddress string
	var probeAddress string
	var enableLeaderElection bool
	var maxLeaseDurationValue string

	flag.StringVar(&metricsAddress, "metrics-bind-address", "0", "Address for the metrics endpoint; use 0 to disable it.")
	flag.StringVar(&probeAddress, "health-probe-bind-address", ":8081", "Address for health probes.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false, "Enable leader election.")
	flag.StringVar(&maxLeaseDurationValue, "max-lease-duration", "30d", "Maximum OverrideLease duration (supports ms, s, m, h, d, and w).")

	logOptions := zap.Options{Development: false}
	logOptions.BindFlags(flag.CommandLine)
	flag.Parse()
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&logOptions)))

	maxLeaseDuration, err := controller.ParseLeaseDuration(maxLeaseDurationValue)
	if err != nil {
		setupLog.Error(err, "invalid max lease duration", "value", maxLeaseDurationValue)
		os.Exit(1)
	}

	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(overridev1alpha1.AddToScheme(scheme))

	manager, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: metricsAddress,
		},
		HealthProbeBindAddress:        probeAddress,
		LeaderElection:                enableLeaderElection,
		LeaderElectionID:              "override-lease.mitsu3s.dev",
		LeaderElectionReleaseOnCancel: true,
	})
	if err != nil {
		setupLog.Error(err, "unable to create manager")
		os.Exit(1)
	}

	reconciler := &controller.OverrideLeaseReconciler{
		Client:           manager.GetClient(),
		Scheme:           manager.GetScheme(),
		Recorder:         manager.GetEventRecorder("override-lease-controller"),
		MaxLeaseDuration: maxLeaseDuration,
	}
	if err := reconciler.SetupWithManager(manager); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "OverrideLease")
		os.Exit(1)
	}
	if err := manager.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to configure health check")
		os.Exit(1)
	}
	if err := manager.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to configure readiness check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := manager.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "manager stopped with an error")
		os.Exit(1)
	}
}
