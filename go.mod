module github.com/bazelbuild/reclient

go 1.23.1 // Remember to also update the go sdks in MODULE.bazel

toolchain go1.23.4

require (
	cloud.google.com/go/bigquery v1.65.0
	cloud.google.com/go/monitoring v1.22.1
	cloud.google.com/go/profiler v0.4.2
	cloud.google.com/go/storage v1.43.0
	cloud.google.com/go/trace v1.11.3
	contrib.go.opencensus.io/exporter/stackdriver v0.13.14
	github.com/GoogleCloudPlatform/protoc-gen-bq-schema v1.1.0
	github.com/GoogleCloudPlatform/protoc-gen-bq-schema/v3 v3.1.0
	github.com/Microsoft/go-winio v0.6.2
	github.com/bazelbuild/remote-apis-sdks v0.0.0-20250708204855-85b23cd12656
	github.com/bazelbuild/rules_go v0.51.0
	github.com/eapache/go-resiliency v1.7.0
	github.com/fatih/color v1.18.0
	github.com/golang/glog v1.2.4
	github.com/google/go-cmp v0.6.0
	github.com/google/uuid v1.6.0
	github.com/gosuri/uilive v0.0.4
	github.com/hectane/go-acl v0.0.0-20230122075934-ca0b05cb1adb
	github.com/karrick/godirwalk v1.17.0
	github.com/kolesnikovae/go-winjob v1.0.0
	github.com/pkg/xattr v0.4.10
	github.com/shirou/gopsutil/v3 v3.24.5
	github.com/vardius/progress-go v0.0.0-20221030221608-f948426036a9
	go.opencensus.io v0.24.0
	golang.org/x/mod v0.22.0
	golang.org/x/oauth2 v0.24.0
	golang.org/x/sync v0.11.0
	golang.org/x/sys v0.30.0
	golang.org/x/tools v0.28.0
	google.golang.org/api v0.214.0
	google.golang.org/genproto v0.0.0-20250106144421-5f5ef82da422
	google.golang.org/genproto/googleapis/rpc v0.0.0-20250207221924-e9438ea467c6
	google.golang.org/grpc v1.68.0
	google.golang.org/protobuf v1.36.4
)

require (
	cloud.google.com/go v0.118.0 // indirect
	cloud.google.com/go/auth v0.13.0 // indirect
	cloud.google.com/go/auth/oauth2adapt v0.2.6 // indirect
	cloud.google.com/go/compute/metadata v0.6.0 // indirect
	cloud.google.com/go/iam v1.3.1 // indirect
	cloud.google.com/go/longrunning v0.6.4 // indirect
	github.com/apache/arrow/go/v15 v15.0.2 // indirect
	github.com/aws/aws-sdk-go v1.43.31 // indirect
	github.com/bazelbuild/remote-apis v0.0.0-20230411132548-35aee1c4a425 // indirect
	github.com/census-instrumentation/opencensus-proto v0.4.1 // indirect
	github.com/felixge/httpsnoop v1.0.4 // indirect
	github.com/go-logr/logr v1.4.2 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/go-ole/go-ole v1.2.6 // indirect
	github.com/goccy/go-json v0.10.2 // indirect
	github.com/golang/groupcache v0.0.0-20210331224755-41bb18bfe9da // indirect
	github.com/golang/protobuf v1.5.4 // indirect
	github.com/google/flatbuffers v23.5.26+incompatible // indirect
	github.com/google/pprof v0.0.0-20240727154555-813a5fbdbec8 // indirect
	github.com/google/s2a-go v0.1.8 // indirect
	github.com/googleapis/enterprise-certificate-proxy v0.3.4 // indirect
	github.com/googleapis/gax-go/v2 v2.14.0 // indirect
	github.com/jmespath/go-jmespath v0.4.0 // indirect
	github.com/klauspost/compress v1.17.8 // indirect
	github.com/klauspost/cpuid/v2 v2.2.5 // indirect
	github.com/lufia/plan9stats v0.0.0-20211012122336-39d0f177ccd0 // indirect
	github.com/mattn/go-colorable v0.1.13 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/pierrec/lz4/v4 v4.1.18 // indirect
	github.com/power-devops/perfstat v0.0.0-20210106213030-5aafc221ea8c // indirect
	github.com/prometheus/prometheus v0.35.0 // indirect
	github.com/shoenig/go-m1cpu v0.1.6 // indirect
	github.com/tklauser/go-sysconf v0.3.12 // indirect
	github.com/tklauser/numcpus v0.6.1 // indirect
	github.com/yusufpapurcu/wmi v1.2.4 // indirect
	github.com/zeebo/xxh3 v1.0.2 // indirect
	go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc v0.54.0 // indirect
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.54.0 // indirect
	go.opentelemetry.io/otel v1.29.0 // indirect
	go.opentelemetry.io/otel/metric v1.29.0 // indirect
	go.opentelemetry.io/otel/trace v1.29.0 // indirect
	golang.org/x/crypto v0.35.0 // indirect
	golang.org/x/exp v0.0.0-20231006140011-7918f672742d // indirect
	golang.org/x/net v0.36.0 // indirect
	golang.org/x/text v0.22.0 // indirect
	golang.org/x/time v0.8.0 // indirect
	golang.org/x/xerrors v0.0.0-20240903120638-7835f813f4da // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20250102185135-69823020774d // indirect
	google.golang.org/genproto/googleapis/bytestream v0.0.0-20241209162323-e6fa225c2576 // indirect
)
