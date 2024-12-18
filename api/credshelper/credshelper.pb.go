// Code generated by protoc-gen-go. DO NOT EDIT.
// versions:
// 	protoc-gen-go v1.36.0
// 	protoc        v5.29.1
// source: api/credshelper/credshelper.proto

package credshelper

import (
	protoreflect "google.golang.org/protobuf/reflect/protoreflect"
	protoimpl "google.golang.org/protobuf/runtime/protoimpl"
	timestamppb "google.golang.org/protobuf/types/known/timestamppb"
	reflect "reflect"
	sync "sync"
)

const (
	// Verify that this generated code is sufficiently up-to-date.
	_ = protoimpl.EnforceVersion(20 - protoimpl.MinVersion)
	// Verify that runtime/protoimpl is sufficiently up-to-date.
	_ = protoimpl.EnforceVersion(protoimpl.MaxVersion - 20)
)

type CachedCredentials struct {
	state         protoimpl.MessageState `protogen:"open.v1"`
	Mechanism     string                 `protobuf:"bytes,1,opt,name=mechanism,proto3" json:"mechanism,omitempty"`
	Token         string                 `protobuf:"bytes,2,opt,name=token,proto3" json:"token,omitempty"`
	Expiry        *timestamppb.Timestamp `protobuf:"bytes,3,opt,name=expiry,proto3" json:"expiry,omitempty"`
	RefreshExpiry *timestamppb.Timestamp `protobuf:"bytes,4,opt,name=refresh_expiry,json=refreshExpiry,proto3" json:"refresh_expiry,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *CachedCredentials) Reset() {
	*x = CachedCredentials{}
	mi := &file_api_credshelper_credshelper_proto_msgTypes[0]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *CachedCredentials) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*CachedCredentials) ProtoMessage() {}

func (x *CachedCredentials) ProtoReflect() protoreflect.Message {
	mi := &file_api_credshelper_credshelper_proto_msgTypes[0]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use CachedCredentials.ProtoReflect.Descriptor instead.
func (*CachedCredentials) Descriptor() ([]byte, []int) {
	return file_api_credshelper_credshelper_proto_rawDescGZIP(), []int{0}
}

func (x *CachedCredentials) GetMechanism() string {
	if x != nil {
		return x.Mechanism
	}
	return ""
}

func (x *CachedCredentials) GetToken() string {
	if x != nil {
		return x.Token
	}
	return ""
}

func (x *CachedCredentials) GetExpiry() *timestamppb.Timestamp {
	if x != nil {
		return x.Expiry
	}
	return nil
}

func (x *CachedCredentials) GetRefreshExpiry() *timestamppb.Timestamp {
	if x != nil {
		return x.RefreshExpiry
	}
	return nil
}

var File_api_credshelper_credshelper_proto protoreflect.FileDescriptor

var file_api_credshelper_credshelper_proto_rawDesc = []byte{
	0x0a, 0x21, 0x61, 0x70, 0x69, 0x2f, 0x63, 0x72, 0x65, 0x64, 0x73, 0x68, 0x65, 0x6c, 0x70, 0x65,
	0x72, 0x2f, 0x63, 0x72, 0x65, 0x64, 0x73, 0x68, 0x65, 0x6c, 0x70, 0x65, 0x72, 0x2e, 0x70, 0x72,
	0x6f, 0x74, 0x6f, 0x12, 0x0b, 0x63, 0x72, 0x65, 0x64, 0x73, 0x68, 0x65, 0x6c, 0x70, 0x65, 0x72,
	0x1a, 0x1f, 0x67, 0x6f, 0x6f, 0x67, 0x6c, 0x65, 0x2f, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x62, 0x75,
	0x66, 0x2f, 0x74, 0x69, 0x6d, 0x65, 0x73, 0x74, 0x61, 0x6d, 0x70, 0x2e, 0x70, 0x72, 0x6f, 0x74,
	0x6f, 0x22, 0xbe, 0x01, 0x0a, 0x11, 0x43, 0x61, 0x63, 0x68, 0x65, 0x64, 0x43, 0x72, 0x65, 0x64,
	0x65, 0x6e, 0x74, 0x69, 0x61, 0x6c, 0x73, 0x12, 0x1c, 0x0a, 0x09, 0x6d, 0x65, 0x63, 0x68, 0x61,
	0x6e, 0x69, 0x73, 0x6d, 0x18, 0x01, 0x20, 0x01, 0x28, 0x09, 0x52, 0x09, 0x6d, 0x65, 0x63, 0x68,
	0x61, 0x6e, 0x69, 0x73, 0x6d, 0x12, 0x14, 0x0a, 0x05, 0x74, 0x6f, 0x6b, 0x65, 0x6e, 0x18, 0x02,
	0x20, 0x01, 0x28, 0x09, 0x52, 0x05, 0x74, 0x6f, 0x6b, 0x65, 0x6e, 0x12, 0x32, 0x0a, 0x06, 0x65,
	0x78, 0x70, 0x69, 0x72, 0x79, 0x18, 0x03, 0x20, 0x01, 0x28, 0x0b, 0x32, 0x1a, 0x2e, 0x67, 0x6f,
	0x6f, 0x67, 0x6c, 0x65, 0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x62, 0x75, 0x66, 0x2e, 0x54, 0x69,
	0x6d, 0x65, 0x73, 0x74, 0x61, 0x6d, 0x70, 0x52, 0x06, 0x65, 0x78, 0x70, 0x69, 0x72, 0x79, 0x12,
	0x41, 0x0a, 0x0e, 0x72, 0x65, 0x66, 0x72, 0x65, 0x73, 0x68, 0x5f, 0x65, 0x78, 0x70, 0x69, 0x72,
	0x79, 0x18, 0x04, 0x20, 0x01, 0x28, 0x0b, 0x32, 0x1a, 0x2e, 0x67, 0x6f, 0x6f, 0x67, 0x6c, 0x65,
	0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x62, 0x75, 0x66, 0x2e, 0x54, 0x69, 0x6d, 0x65, 0x73, 0x74,
	0x61, 0x6d, 0x70, 0x52, 0x0d, 0x72, 0x65, 0x66, 0x72, 0x65, 0x73, 0x68, 0x45, 0x78, 0x70, 0x69,
	0x72, 0x79, 0x62, 0x06, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x33,
}

var (
	file_api_credshelper_credshelper_proto_rawDescOnce sync.Once
	file_api_credshelper_credshelper_proto_rawDescData = file_api_credshelper_credshelper_proto_rawDesc
)

func file_api_credshelper_credshelper_proto_rawDescGZIP() []byte {
	file_api_credshelper_credshelper_proto_rawDescOnce.Do(func() {
		file_api_credshelper_credshelper_proto_rawDescData = protoimpl.X.CompressGZIP(file_api_credshelper_credshelper_proto_rawDescData)
	})
	return file_api_credshelper_credshelper_proto_rawDescData
}

var file_api_credshelper_credshelper_proto_msgTypes = make([]protoimpl.MessageInfo, 1)
var file_api_credshelper_credshelper_proto_goTypes = []any{
	(*CachedCredentials)(nil),     // 0: credshelper.CachedCredentials
	(*timestamppb.Timestamp)(nil), // 1: google.protobuf.Timestamp
}
var file_api_credshelper_credshelper_proto_depIdxs = []int32{
	1, // 0: credshelper.CachedCredentials.expiry:type_name -> google.protobuf.Timestamp
	1, // 1: credshelper.CachedCredentials.refresh_expiry:type_name -> google.protobuf.Timestamp
	2, // [2:2] is the sub-list for method output_type
	2, // [2:2] is the sub-list for method input_type
	2, // [2:2] is the sub-list for extension type_name
	2, // [2:2] is the sub-list for extension extendee
	0, // [0:2] is the sub-list for field type_name
}

func init() { file_api_credshelper_credshelper_proto_init() }
func file_api_credshelper_credshelper_proto_init() {
	if File_api_credshelper_credshelper_proto != nil {
		return
	}
	type x struct{}
	out := protoimpl.TypeBuilder{
		File: protoimpl.DescBuilder{
			GoPackagePath: reflect.TypeOf(x{}).PkgPath(),
			RawDescriptor: file_api_credshelper_credshelper_proto_rawDesc,
			NumEnums:      0,
			NumMessages:   1,
			NumExtensions: 0,
			NumServices:   0,
		},
		GoTypes:           file_api_credshelper_credshelper_proto_goTypes,
		DependencyIndexes: file_api_credshelper_credshelper_proto_depIdxs,
		MessageInfos:      file_api_credshelper_credshelper_proto_msgTypes,
	}.Build()
	File_api_credshelper_credshelper_proto = out.File
	file_api_credshelper_credshelper_proto_rawDesc = nil
	file_api_credshelper_credshelper_proto_goTypes = nil
	file_api_credshelper_credshelper_proto_depIdxs = nil
}
