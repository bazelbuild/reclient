// Code generated by protoc-gen-go. DO NOT EDIT.
// versions:
// 	protoc-gen-go v1.35.1
// 	protoc        v5.26.0
// source: api/stats/stats.proto

package stats

import (
	log "github.com/bazelbuild/reclient/api/log"
	stat "github.com/bazelbuild/reclient/api/stat"
	protoreflect "google.golang.org/protobuf/reflect/protoreflect"
	protoimpl "google.golang.org/protobuf/runtime/protoimpl"
	reflect "reflect"
	sync "sync"
)

const (
	// Verify that this generated code is sufficiently up-to-date.
	_ = protoimpl.EnforceVersion(20 - protoimpl.MinVersion)
	// Verify that runtime/protoimpl is sufficiently up-to-date.
	_ = protoimpl.EnforceVersion(protoimpl.MaxVersion - 20)
)

type Stats struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	NumRecords         int64             `protobuf:"varint,1,opt,name=num_records,json=numRecords,proto3" json:"num_records,omitempty"`
	Stats              []*stat.Stat      `protobuf:"bytes,2,rep,name=stats,proto3" json:"stats,omitempty"`
	Verification       *log.Verification `protobuf:"bytes,4,opt,name=verification,proto3" json:"verification,omitempty"`
	ToolVersion        string            `protobuf:"bytes,5,opt,name=tool_version,json=toolVersion,proto3" json:"tool_version,omitempty"`
	InvocationIds      []string          `protobuf:"bytes,6,rep,name=invocation_ids,json=invocationIds,proto3" json:"invocation_ids,omitempty"`
	MachineInfo        *MachineInfo      `protobuf:"bytes,7,opt,name=machine_info,json=machineInfo,proto3" json:"machine_info,omitempty"`
	ProxyInfo          []*log.ProxyInfo  `protobuf:"bytes,9,rep,name=proxy_info,json=proxyInfo,proto3" json:"proxy_info,omitempty"`
	BuildCacheHitRatio float64           `protobuf:"fixed64,10,opt,name=build_cache_hit_ratio,json=buildCacheHitRatio,proto3" json:"build_cache_hit_ratio,omitempty"`
	BuildLatency       float64           `protobuf:"fixed64,11,opt,name=build_latency,json=buildLatency,proto3" json:"build_latency,omitempty"`
	FatalExit          bool              `protobuf:"varint,12,opt,name=fatal_exit,json=fatalExit,proto3" json:"fatal_exit,omitempty"`
	LogDirectories     []*LogDirectory   `protobuf:"bytes,13,rep,name=log_directories,json=logDirectories,proto3" json:"log_directories,omitempty"`
}

func (x *Stats) Reset() {
	*x = Stats{}
	mi := &file_api_stats_stats_proto_msgTypes[0]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *Stats) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*Stats) ProtoMessage() {}

func (x *Stats) ProtoReflect() protoreflect.Message {
	mi := &file_api_stats_stats_proto_msgTypes[0]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use Stats.ProtoReflect.Descriptor instead.
func (*Stats) Descriptor() ([]byte, []int) {
	return file_api_stats_stats_proto_rawDescGZIP(), []int{0}
}

func (x *Stats) GetNumRecords() int64 {
	if x != nil {
		return x.NumRecords
	}
	return 0
}

func (x *Stats) GetStats() []*stat.Stat {
	if x != nil {
		return x.Stats
	}
	return nil
}

func (x *Stats) GetVerification() *log.Verification {
	if x != nil {
		return x.Verification
	}
	return nil
}

func (x *Stats) GetToolVersion() string {
	if x != nil {
		return x.ToolVersion
	}
	return ""
}

func (x *Stats) GetInvocationIds() []string {
	if x != nil {
		return x.InvocationIds
	}
	return nil
}

func (x *Stats) GetMachineInfo() *MachineInfo {
	if x != nil {
		return x.MachineInfo
	}
	return nil
}

func (x *Stats) GetProxyInfo() []*log.ProxyInfo {
	if x != nil {
		return x.ProxyInfo
	}
	return nil
}

func (x *Stats) GetBuildCacheHitRatio() float64 {
	if x != nil {
		return x.BuildCacheHitRatio
	}
	return 0
}

func (x *Stats) GetBuildLatency() float64 {
	if x != nil {
		return x.BuildLatency
	}
	return 0
}

func (x *Stats) GetFatalExit() bool {
	if x != nil {
		return x.FatalExit
	}
	return false
}

func (x *Stats) GetLogDirectories() []*LogDirectory {
	if x != nil {
		return x.LogDirectories
	}
	return nil
}

type LogDirectory struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	Path   string `protobuf:"bytes,1,opt,name=path,proto3" json:"path,omitempty"`
	Digest string `protobuf:"bytes,2,opt,name=digest,proto3" json:"digest,omitempty"`
}

func (x *LogDirectory) Reset() {
	*x = LogDirectory{}
	mi := &file_api_stats_stats_proto_msgTypes[1]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *LogDirectory) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*LogDirectory) ProtoMessage() {}

func (x *LogDirectory) ProtoReflect() protoreflect.Message {
	mi := &file_api_stats_stats_proto_msgTypes[1]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use LogDirectory.ProtoReflect.Descriptor instead.
func (*LogDirectory) Descriptor() ([]byte, []int) {
	return file_api_stats_stats_proto_rawDescGZIP(), []int{1}
}

func (x *LogDirectory) GetPath() string {
	if x != nil {
		return x.Path
	}
	return ""
}

func (x *LogDirectory) GetDigest() string {
	if x != nil {
		return x.Digest
	}
	return ""
}

type MachineInfo struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	NumCpu   int64  `protobuf:"varint,1,opt,name=num_cpu,json=numCpu,proto3" json:"num_cpu,omitempty"`
	RamMbs   int64  `protobuf:"varint,2,opt,name=ram_mbs,json=ramMbs,proto3" json:"ram_mbs,omitempty"`
	OsFamily string `protobuf:"bytes,3,opt,name=os_family,json=osFamily,proto3" json:"os_family,omitempty"`
	Arch     string `protobuf:"bytes,4,opt,name=arch,proto3" json:"arch,omitempty"`
}

func (x *MachineInfo) Reset() {
	*x = MachineInfo{}
	mi := &file_api_stats_stats_proto_msgTypes[2]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *MachineInfo) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*MachineInfo) ProtoMessage() {}

func (x *MachineInfo) ProtoReflect() protoreflect.Message {
	mi := &file_api_stats_stats_proto_msgTypes[2]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use MachineInfo.ProtoReflect.Descriptor instead.
func (*MachineInfo) Descriptor() ([]byte, []int) {
	return file_api_stats_stats_proto_rawDescGZIP(), []int{2}
}

func (x *MachineInfo) GetNumCpu() int64 {
	if x != nil {
		return x.NumCpu
	}
	return 0
}

func (x *MachineInfo) GetRamMbs() int64 {
	if x != nil {
		return x.RamMbs
	}
	return 0
}

func (x *MachineInfo) GetOsFamily() string {
	if x != nil {
		return x.OsFamily
	}
	return ""
}

func (x *MachineInfo) GetArch() string {
	if x != nil {
		return x.Arch
	}
	return ""
}

var File_api_stats_stats_proto protoreflect.FileDescriptor

var file_api_stats_stats_proto_rawDesc = []byte{
	0x0a, 0x15, 0x61, 0x70, 0x69, 0x2f, 0x73, 0x74, 0x61, 0x74, 0x73, 0x2f, 0x73, 0x74, 0x61, 0x74,
	0x73, 0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x12, 0x05, 0x73, 0x74, 0x61, 0x74, 0x73, 0x1a, 0x11,
	0x61, 0x70, 0x69, 0x2f, 0x6c, 0x6f, 0x67, 0x2f, 0x6c, 0x6f, 0x67, 0x2e, 0x70, 0x72, 0x6f, 0x74,
	0x6f, 0x1a, 0x13, 0x61, 0x70, 0x69, 0x2f, 0x73, 0x74, 0x61, 0x74, 0x2f, 0x73, 0x74, 0x61, 0x74,
	0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x22, 0xf3, 0x03, 0x0a, 0x05, 0x53, 0x74, 0x61, 0x74, 0x73,
	0x12, 0x1f, 0x0a, 0x0b, 0x6e, 0x75, 0x6d, 0x5f, 0x72, 0x65, 0x63, 0x6f, 0x72, 0x64, 0x73, 0x18,
	0x01, 0x20, 0x01, 0x28, 0x03, 0x52, 0x0a, 0x6e, 0x75, 0x6d, 0x52, 0x65, 0x63, 0x6f, 0x72, 0x64,
	0x73, 0x12, 0x21, 0x0a, 0x05, 0x73, 0x74, 0x61, 0x74, 0x73, 0x18, 0x02, 0x20, 0x03, 0x28, 0x0b,
	0x32, 0x0b, 0x2e, 0x73, 0x74, 0x61, 0x74, 0x73, 0x2e, 0x53, 0x74, 0x61, 0x74, 0x52, 0x05, 0x73,
	0x74, 0x61, 0x74, 0x73, 0x12, 0x35, 0x0a, 0x0c, 0x76, 0x65, 0x72, 0x69, 0x66, 0x69, 0x63, 0x61,
	0x74, 0x69, 0x6f, 0x6e, 0x18, 0x04, 0x20, 0x01, 0x28, 0x0b, 0x32, 0x11, 0x2e, 0x6c, 0x6f, 0x67,
	0x2e, 0x56, 0x65, 0x72, 0x69, 0x66, 0x69, 0x63, 0x61, 0x74, 0x69, 0x6f, 0x6e, 0x52, 0x0c, 0x76,
	0x65, 0x72, 0x69, 0x66, 0x69, 0x63, 0x61, 0x74, 0x69, 0x6f, 0x6e, 0x12, 0x21, 0x0a, 0x0c, 0x74,
	0x6f, 0x6f, 0x6c, 0x5f, 0x76, 0x65, 0x72, 0x73, 0x69, 0x6f, 0x6e, 0x18, 0x05, 0x20, 0x01, 0x28,
	0x09, 0x52, 0x0b, 0x74, 0x6f, 0x6f, 0x6c, 0x56, 0x65, 0x72, 0x73, 0x69, 0x6f, 0x6e, 0x12, 0x25,
	0x0a, 0x0e, 0x69, 0x6e, 0x76, 0x6f, 0x63, 0x61, 0x74, 0x69, 0x6f, 0x6e, 0x5f, 0x69, 0x64, 0x73,
	0x18, 0x06, 0x20, 0x03, 0x28, 0x09, 0x52, 0x0d, 0x69, 0x6e, 0x76, 0x6f, 0x63, 0x61, 0x74, 0x69,
	0x6f, 0x6e, 0x49, 0x64, 0x73, 0x12, 0x35, 0x0a, 0x0c, 0x6d, 0x61, 0x63, 0x68, 0x69, 0x6e, 0x65,
	0x5f, 0x69, 0x6e, 0x66, 0x6f, 0x18, 0x07, 0x20, 0x01, 0x28, 0x0b, 0x32, 0x12, 0x2e, 0x73, 0x74,
	0x61, 0x74, 0x73, 0x2e, 0x4d, 0x61, 0x63, 0x68, 0x69, 0x6e, 0x65, 0x49, 0x6e, 0x66, 0x6f, 0x52,
	0x0b, 0x6d, 0x61, 0x63, 0x68, 0x69, 0x6e, 0x65, 0x49, 0x6e, 0x66, 0x6f, 0x12, 0x2d, 0x0a, 0x0a,
	0x70, 0x72, 0x6f, 0x78, 0x79, 0x5f, 0x69, 0x6e, 0x66, 0x6f, 0x18, 0x09, 0x20, 0x03, 0x28, 0x0b,
	0x32, 0x0e, 0x2e, 0x6c, 0x6f, 0x67, 0x2e, 0x50, 0x72, 0x6f, 0x78, 0x79, 0x49, 0x6e, 0x66, 0x6f,
	0x52, 0x09, 0x70, 0x72, 0x6f, 0x78, 0x79, 0x49, 0x6e, 0x66, 0x6f, 0x12, 0x31, 0x0a, 0x15, 0x62,
	0x75, 0x69, 0x6c, 0x64, 0x5f, 0x63, 0x61, 0x63, 0x68, 0x65, 0x5f, 0x68, 0x69, 0x74, 0x5f, 0x72,
	0x61, 0x74, 0x69, 0x6f, 0x18, 0x0a, 0x20, 0x01, 0x28, 0x01, 0x52, 0x12, 0x62, 0x75, 0x69, 0x6c,
	0x64, 0x43, 0x61, 0x63, 0x68, 0x65, 0x48, 0x69, 0x74, 0x52, 0x61, 0x74, 0x69, 0x6f, 0x12, 0x23,
	0x0a, 0x0d, 0x62, 0x75, 0x69, 0x6c, 0x64, 0x5f, 0x6c, 0x61, 0x74, 0x65, 0x6e, 0x63, 0x79, 0x18,
	0x0b, 0x20, 0x01, 0x28, 0x01, 0x52, 0x0c, 0x62, 0x75, 0x69, 0x6c, 0x64, 0x4c, 0x61, 0x74, 0x65,
	0x6e, 0x63, 0x79, 0x12, 0x1d, 0x0a, 0x0a, 0x66, 0x61, 0x74, 0x61, 0x6c, 0x5f, 0x65, 0x78, 0x69,
	0x74, 0x18, 0x0c, 0x20, 0x01, 0x28, 0x08, 0x52, 0x09, 0x66, 0x61, 0x74, 0x61, 0x6c, 0x45, 0x78,
	0x69, 0x74, 0x12, 0x3c, 0x0a, 0x0f, 0x6c, 0x6f, 0x67, 0x5f, 0x64, 0x69, 0x72, 0x65, 0x63, 0x74,
	0x6f, 0x72, 0x69, 0x65, 0x73, 0x18, 0x0d, 0x20, 0x03, 0x28, 0x0b, 0x32, 0x13, 0x2e, 0x73, 0x74,
	0x61, 0x74, 0x73, 0x2e, 0x4c, 0x6f, 0x67, 0x44, 0x69, 0x72, 0x65, 0x63, 0x74, 0x6f, 0x72, 0x79,
	0x52, 0x0e, 0x6c, 0x6f, 0x67, 0x44, 0x69, 0x72, 0x65, 0x63, 0x74, 0x6f, 0x72, 0x69, 0x65, 0x73,
	0x4a, 0x04, 0x08, 0x03, 0x10, 0x04, 0x4a, 0x04, 0x08, 0x08, 0x10, 0x09, 0x22, 0x3a, 0x0a, 0x0c,
	0x4c, 0x6f, 0x67, 0x44, 0x69, 0x72, 0x65, 0x63, 0x74, 0x6f, 0x72, 0x79, 0x12, 0x12, 0x0a, 0x04,
	0x70, 0x61, 0x74, 0x68, 0x18, 0x01, 0x20, 0x01, 0x28, 0x09, 0x52, 0x04, 0x70, 0x61, 0x74, 0x68,
	0x12, 0x16, 0x0a, 0x06, 0x64, 0x69, 0x67, 0x65, 0x73, 0x74, 0x18, 0x02, 0x20, 0x01, 0x28, 0x09,
	0x52, 0x06, 0x64, 0x69, 0x67, 0x65, 0x73, 0x74, 0x22, 0x70, 0x0a, 0x0b, 0x4d, 0x61, 0x63, 0x68,
	0x69, 0x6e, 0x65, 0x49, 0x6e, 0x66, 0x6f, 0x12, 0x17, 0x0a, 0x07, 0x6e, 0x75, 0x6d, 0x5f, 0x63,
	0x70, 0x75, 0x18, 0x01, 0x20, 0x01, 0x28, 0x03, 0x52, 0x06, 0x6e, 0x75, 0x6d, 0x43, 0x70, 0x75,
	0x12, 0x17, 0x0a, 0x07, 0x72, 0x61, 0x6d, 0x5f, 0x6d, 0x62, 0x73, 0x18, 0x02, 0x20, 0x01, 0x28,
	0x03, 0x52, 0x06, 0x72, 0x61, 0x6d, 0x4d, 0x62, 0x73, 0x12, 0x1b, 0x0a, 0x09, 0x6f, 0x73, 0x5f,
	0x66, 0x61, 0x6d, 0x69, 0x6c, 0x79, 0x18, 0x03, 0x20, 0x01, 0x28, 0x09, 0x52, 0x08, 0x6f, 0x73,
	0x46, 0x61, 0x6d, 0x69, 0x6c, 0x79, 0x12, 0x12, 0x0a, 0x04, 0x61, 0x72, 0x63, 0x68, 0x18, 0x04,
	0x20, 0x01, 0x28, 0x09, 0x52, 0x04, 0x61, 0x72, 0x63, 0x68, 0x42, 0x2a, 0x5a, 0x28, 0x67, 0x69,
	0x74, 0x68, 0x75, 0x62, 0x2e, 0x63, 0x6f, 0x6d, 0x2f, 0x62, 0x61, 0x7a, 0x65, 0x6c, 0x62, 0x75,
	0x69, 0x6c, 0x64, 0x2f, 0x72, 0x65, 0x63, 0x6c, 0x69, 0x65, 0x6e, 0x74, 0x2f, 0x61, 0x70, 0x69,
	0x2f, 0x73, 0x74, 0x61, 0x74, 0x73, 0x62, 0x06, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x33,
}

var (
	file_api_stats_stats_proto_rawDescOnce sync.Once
	file_api_stats_stats_proto_rawDescData = file_api_stats_stats_proto_rawDesc
)

func file_api_stats_stats_proto_rawDescGZIP() []byte {
	file_api_stats_stats_proto_rawDescOnce.Do(func() {
		file_api_stats_stats_proto_rawDescData = protoimpl.X.CompressGZIP(file_api_stats_stats_proto_rawDescData)
	})
	return file_api_stats_stats_proto_rawDescData
}

var file_api_stats_stats_proto_msgTypes = make([]protoimpl.MessageInfo, 3)
var file_api_stats_stats_proto_goTypes = []any{
	(*Stats)(nil),            // 0: stats.Stats
	(*LogDirectory)(nil),     // 1: stats.LogDirectory
	(*MachineInfo)(nil),      // 2: stats.MachineInfo
	(*stat.Stat)(nil),        // 3: stats.Stat
	(*log.Verification)(nil), // 4: log.Verification
	(*log.ProxyInfo)(nil),    // 5: log.ProxyInfo
}
var file_api_stats_stats_proto_depIdxs = []int32{
	3, // 0: stats.Stats.stats:type_name -> stats.Stat
	4, // 1: stats.Stats.verification:type_name -> log.Verification
	2, // 2: stats.Stats.machine_info:type_name -> stats.MachineInfo
	5, // 3: stats.Stats.proxy_info:type_name -> log.ProxyInfo
	1, // 4: stats.Stats.log_directories:type_name -> stats.LogDirectory
	5, // [5:5] is the sub-list for method output_type
	5, // [5:5] is the sub-list for method input_type
	5, // [5:5] is the sub-list for extension type_name
	5, // [5:5] is the sub-list for extension extendee
	0, // [0:5] is the sub-list for field type_name
}

func init() { file_api_stats_stats_proto_init() }
func file_api_stats_stats_proto_init() {
	if File_api_stats_stats_proto != nil {
		return
	}
	type x struct{}
	out := protoimpl.TypeBuilder{
		File: protoimpl.DescBuilder{
			GoPackagePath: reflect.TypeOf(x{}).PkgPath(),
			RawDescriptor: file_api_stats_stats_proto_rawDesc,
			NumEnums:      0,
			NumMessages:   3,
			NumExtensions: 0,
			NumServices:   0,
		},
		GoTypes:           file_api_stats_stats_proto_goTypes,
		DependencyIndexes: file_api_stats_stats_proto_depIdxs,
		MessageInfos:      file_api_stats_stats_proto_msgTypes,
	}.Build()
	File_api_stats_stats_proto = out.File
	file_api_stats_stats_proto_rawDesc = nil
	file_api_stats_stats_proto_goTypes = nil
	file_api_stats_stats_proto_depIdxs = nil
}
