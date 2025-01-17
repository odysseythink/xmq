// Code generated by protoc-gen-go. DO NOT EDIT.
// source: common.proto

package pbapi

import (
	fmt "fmt"
	proto "github.com/golang/protobuf/proto"
	math "math"
)

// Reference imports to suppress errors if they are not otherwise used.
var _ = proto.Marshal
var _ = fmt.Errorf
var _ = math.Inf

// This is a compile-time assertion to ensure that this generated file
// is compatible with the proto package it is being compiled against.
// A compilation error at this line likely means your copy of the
// proto package needs to be updated.
const _ = proto.ProtoPackageIsVersion3 // please upgrade the proto package

type PingRequest struct {
	Peer                 *PeerInfo `protobuf:"bytes,1,opt,name=peer,proto3" json:"peer,omitempty"`
	XXX_NoUnkeyedLiteral struct{}  `json:"-"`
	XXX_unrecognized     []byte    `json:"-"`
	XXX_sizecache        int32     `json:"-"`
}

func (m *PingRequest) Reset()         { *m = PingRequest{} }
func (m *PingRequest) String() string { return proto.CompactTextString(m) }
func (*PingRequest) ProtoMessage()    {}
func (*PingRequest) Descriptor() ([]byte, []int) {
	return fileDescriptor_555bd8c177793206, []int{0}
}

func (m *PingRequest) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_PingRequest.Unmarshal(m, b)
}
func (m *PingRequest) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_PingRequest.Marshal(b, m, deterministic)
}
func (m *PingRequest) XXX_Merge(src proto.Message) {
	xxx_messageInfo_PingRequest.Merge(m, src)
}
func (m *PingRequest) XXX_Size() int {
	return xxx_messageInfo_PingRequest.Size(m)
}
func (m *PingRequest) XXX_DiscardUnknown() {
	xxx_messageInfo_PingRequest.DiscardUnknown(m)
}

var xxx_messageInfo_PingRequest proto.InternalMessageInfo

func (m *PingRequest) GetPeer() *PeerInfo {
	if m != nil {
		return m.Peer
	}
	return nil
}

type PingReply struct {
	Err                  string   `protobuf:"bytes,1,opt,name=err,proto3" json:"err,omitempty"`
	Msg                  string   `protobuf:"bytes,2,opt,name=msg,proto3" json:"msg,omitempty"`
	XXX_NoUnkeyedLiteral struct{} `json:"-"`
	XXX_unrecognized     []byte   `json:"-"`
	XXX_sizecache        int32    `json:"-"`
}

func (m *PingReply) Reset()         { *m = PingReply{} }
func (m *PingReply) String() string { return proto.CompactTextString(m) }
func (*PingReply) ProtoMessage()    {}
func (*PingReply) Descriptor() ([]byte, []int) {
	return fileDescriptor_555bd8c177793206, []int{1}
}

func (m *PingReply) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_PingReply.Unmarshal(m, b)
}
func (m *PingReply) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_PingReply.Marshal(b, m, deterministic)
}
func (m *PingReply) XXX_Merge(src proto.Message) {
	xxx_messageInfo_PingReply.Merge(m, src)
}
func (m *PingReply) XXX_Size() int {
	return xxx_messageInfo_PingReply.Size(m)
}
func (m *PingReply) XXX_DiscardUnknown() {
	xxx_messageInfo_PingReply.DiscardUnknown(m)
}

var xxx_messageInfo_PingReply proto.InternalMessageInfo

func (m *PingReply) GetErr() string {
	if m != nil {
		return m.Err
	}
	return ""
}

func (m *PingReply) GetMsg() string {
	if m != nil {
		return m.Msg
	}
	return ""
}

type IdentifyRequest struct {
	Peer                 *PeerInfo `protobuf:"bytes,1,opt,name=peer,proto3" json:"peer,omitempty"`
	XXX_NoUnkeyedLiteral struct{}  `json:"-"`
	XXX_unrecognized     []byte    `json:"-"`
	XXX_sizecache        int32     `json:"-"`
}

func (m *IdentifyRequest) Reset()         { *m = IdentifyRequest{} }
func (m *IdentifyRequest) String() string { return proto.CompactTextString(m) }
func (*IdentifyRequest) ProtoMessage()    {}
func (*IdentifyRequest) Descriptor() ([]byte, []int) {
	return fileDescriptor_555bd8c177793206, []int{2}
}

func (m *IdentifyRequest) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_IdentifyRequest.Unmarshal(m, b)
}
func (m *IdentifyRequest) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_IdentifyRequest.Marshal(b, m, deterministic)
}
func (m *IdentifyRequest) XXX_Merge(src proto.Message) {
	xxx_messageInfo_IdentifyRequest.Merge(m, src)
}
func (m *IdentifyRequest) XXX_Size() int {
	return xxx_messageInfo_IdentifyRequest.Size(m)
}
func (m *IdentifyRequest) XXX_DiscardUnknown() {
	xxx_messageInfo_IdentifyRequest.DiscardUnknown(m)
}

var xxx_messageInfo_IdentifyRequest proto.InternalMessageInfo

func (m *IdentifyRequest) GetPeer() *PeerInfo {
	if m != nil {
		return m.Peer
	}
	return nil
}

type IdentifyReply struct {
	Err                  string   `protobuf:"bytes,1,opt,name=err,proto3" json:"err,omitempty"`
	TcpPort              int32    `protobuf:"varint,2,opt,name=tcp_port,json=tcpPort,proto3" json:"tcp_port,omitempty"`
	HttpPort             int32    `protobuf:"varint,3,opt,name=http_port,json=httpPort,proto3" json:"http_port,omitempty"`
	Version              string   `protobuf:"bytes,4,opt,name=version,proto3" json:"version,omitempty"`
	BroadcastAddress     string   `protobuf:"bytes,5,opt,name=broadcast_address,json=broadcastAddress,proto3" json:"broadcast_address,omitempty"`
	Hostname             string   `protobuf:"bytes,6,opt,name=hostname,proto3" json:"hostname,omitempty"`
	XXX_NoUnkeyedLiteral struct{} `json:"-"`
	XXX_unrecognized     []byte   `json:"-"`
	XXX_sizecache        int32    `json:"-"`
}

func (m *IdentifyReply) Reset()         { *m = IdentifyReply{} }
func (m *IdentifyReply) String() string { return proto.CompactTextString(m) }
func (*IdentifyReply) ProtoMessage()    {}
func (*IdentifyReply) Descriptor() ([]byte, []int) {
	return fileDescriptor_555bd8c177793206, []int{3}
}

func (m *IdentifyReply) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_IdentifyReply.Unmarshal(m, b)
}
func (m *IdentifyReply) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_IdentifyReply.Marshal(b, m, deterministic)
}
func (m *IdentifyReply) XXX_Merge(src proto.Message) {
	xxx_messageInfo_IdentifyReply.Merge(m, src)
}
func (m *IdentifyReply) XXX_Size() int {
	return xxx_messageInfo_IdentifyReply.Size(m)
}
func (m *IdentifyReply) XXX_DiscardUnknown() {
	xxx_messageInfo_IdentifyReply.DiscardUnknown(m)
}

var xxx_messageInfo_IdentifyReply proto.InternalMessageInfo

func (m *IdentifyReply) GetErr() string {
	if m != nil {
		return m.Err
	}
	return ""
}

func (m *IdentifyReply) GetTcpPort() int32 {
	if m != nil {
		return m.TcpPort
	}
	return 0
}

func (m *IdentifyReply) GetHttpPort() int32 {
	if m != nil {
		return m.HttpPort
	}
	return 0
}

func (m *IdentifyReply) GetVersion() string {
	if m != nil {
		return m.Version
	}
	return ""
}

func (m *IdentifyReply) GetBroadcastAddress() string {
	if m != nil {
		return m.BroadcastAddress
	}
	return ""
}

func (m *IdentifyReply) GetHostname() string {
	if m != nil {
		return m.Hostname
	}
	return ""
}

type Channel struct {
	RequeueCount         uint64   `protobuf:"varint,1,opt,name=requeue_count,json=requeueCount,proto3" json:"requeue_count,omitempty"`
	MessageCount         uint64   `protobuf:"varint,2,opt,name=message_count,json=messageCount,proto3" json:"message_count,omitempty"`
	TimeoutCount         uint64   `protobuf:"varint,3,opt,name=timeout_count,json=timeoutCount,proto3" json:"timeout_count,omitempty"`
	TopicName            string   `protobuf:"bytes,4,opt,name=topic_name,json=topicName,proto3" json:"topic_name,omitempty"`
	Name                 string   `protobuf:"bytes,5,opt,name=name,proto3" json:"name,omitempty"`
	ExitFlag             int32    `protobuf:"varint,6,opt,name=exit_flag,json=exitFlag,proto3" json:"exit_flag,omitempty"`
	Paused               int32    `protobuf:"varint,7,opt,name=paused,proto3" json:"paused,omitempty"`
	Ephemeral            bool     `protobuf:"varint,8,opt,name=ephemeral,proto3" json:"ephemeral,omitempty"`
	XXX_NoUnkeyedLiteral struct{} `json:"-"`
	XXX_unrecognized     []byte   `json:"-"`
	XXX_sizecache        int32    `json:"-"`
}

func (m *Channel) Reset()         { *m = Channel{} }
func (m *Channel) String() string { return proto.CompactTextString(m) }
func (*Channel) ProtoMessage()    {}
func (*Channel) Descriptor() ([]byte, []int) {
	return fileDescriptor_555bd8c177793206, []int{4}
}

func (m *Channel) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_Channel.Unmarshal(m, b)
}
func (m *Channel) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_Channel.Marshal(b, m, deterministic)
}
func (m *Channel) XXX_Merge(src proto.Message) {
	xxx_messageInfo_Channel.Merge(m, src)
}
func (m *Channel) XXX_Size() int {
	return xxx_messageInfo_Channel.Size(m)
}
func (m *Channel) XXX_DiscardUnknown() {
	xxx_messageInfo_Channel.DiscardUnknown(m)
}

var xxx_messageInfo_Channel proto.InternalMessageInfo

func (m *Channel) GetRequeueCount() uint64 {
	if m != nil {
		return m.RequeueCount
	}
	return 0
}

func (m *Channel) GetMessageCount() uint64 {
	if m != nil {
		return m.MessageCount
	}
	return 0
}

func (m *Channel) GetTimeoutCount() uint64 {
	if m != nil {
		return m.TimeoutCount
	}
	return 0
}

func (m *Channel) GetTopicName() string {
	if m != nil {
		return m.TopicName
	}
	return ""
}

func (m *Channel) GetName() string {
	if m != nil {
		return m.Name
	}
	return ""
}

func (m *Channel) GetExitFlag() int32 {
	if m != nil {
		return m.ExitFlag
	}
	return 0
}

func (m *Channel) GetPaused() int32 {
	if m != nil {
		return m.Paused
	}
	return 0
}

func (m *Channel) GetEphemeral() bool {
	if m != nil {
		return m.Ephemeral
	}
	return false
}

type Topic struct {
	MessageCount         uint64     `protobuf:"varint,1,opt,name=message_count,json=messageCount,proto3" json:"message_count,omitempty"`
	MessageBytes         uint64     `protobuf:"varint,2,opt,name=message_bytes,json=messageBytes,proto3" json:"message_bytes,omitempty"`
	Name                 string     `protobuf:"bytes,3,opt,name=name,proto3" json:"name,omitempty"`
	Channels             []*Channel `protobuf:"bytes,4,rep,name=channels,proto3" json:"channels,omitempty"`
	ExitFlag             int32      `protobuf:"varint,5,opt,name=exit_flag,json=exitFlag,proto3" json:"exit_flag,omitempty"`
	Ephemeral            bool       `protobuf:"varint,6,opt,name=ephemeral,proto3" json:"ephemeral,omitempty"`
	Paused               int32      `protobuf:"varint,7,opt,name=paused,proto3" json:"paused,omitempty"`
	XXX_NoUnkeyedLiteral struct{}   `json:"-"`
	XXX_unrecognized     []byte     `json:"-"`
	XXX_sizecache        int32      `json:"-"`
}

func (m *Topic) Reset()         { *m = Topic{} }
func (m *Topic) String() string { return proto.CompactTextString(m) }
func (*Topic) ProtoMessage()    {}
func (*Topic) Descriptor() ([]byte, []int) {
	return fileDescriptor_555bd8c177793206, []int{5}
}

func (m *Topic) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_Topic.Unmarshal(m, b)
}
func (m *Topic) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_Topic.Marshal(b, m, deterministic)
}
func (m *Topic) XXX_Merge(src proto.Message) {
	xxx_messageInfo_Topic.Merge(m, src)
}
func (m *Topic) XXX_Size() int {
	return xxx_messageInfo_Topic.Size(m)
}
func (m *Topic) XXX_DiscardUnknown() {
	xxx_messageInfo_Topic.DiscardUnknown(m)
}

var xxx_messageInfo_Topic proto.InternalMessageInfo

func (m *Topic) GetMessageCount() uint64 {
	if m != nil {
		return m.MessageCount
	}
	return 0
}

func (m *Topic) GetMessageBytes() uint64 {
	if m != nil {
		return m.MessageBytes
	}
	return 0
}

func (m *Topic) GetName() string {
	if m != nil {
		return m.Name
	}
	return ""
}

func (m *Topic) GetChannels() []*Channel {
	if m != nil {
		return m.Channels
	}
	return nil
}

func (m *Topic) GetExitFlag() int32 {
	if m != nil {
		return m.ExitFlag
	}
	return 0
}

func (m *Topic) GetEphemeral() bool {
	if m != nil {
		return m.Ephemeral
	}
	return false
}

func (m *Topic) GetPaused() int32 {
	if m != nil {
		return m.Paused
	}
	return 0
}

type PeerInfo struct {
	Id                   string   `protobuf:"bytes,1,opt,name=id,proto3" json:"id,omitempty"`
	RemoteAddress        string   `protobuf:"bytes,2,opt,name=remote_address,json=remoteAddress,proto3" json:"remote_address,omitempty"`
	Hostname             string   `protobuf:"bytes,3,opt,name=hostname,proto3" json:"hostname,omitempty"`
	BroadcastAddress     string   `protobuf:"bytes,4,opt,name=broadcast_address,json=broadcastAddress,proto3" json:"broadcast_address,omitempty"`
	TcpPort              int32    `protobuf:"varint,5,opt,name=tcp_port,json=tcpPort,proto3" json:"tcp_port,omitempty"`
	HttpPort             int32    `protobuf:"varint,6,opt,name=http_port,json=httpPort,proto3" json:"http_port,omitempty"`
	Version              string   `protobuf:"bytes,7,opt,name=version,proto3" json:"version,omitempty"`
	LastUpdate           int64    `protobuf:"varint,8,opt,name=last_update,json=lastUpdate,proto3" json:"last_update,omitempty"`
	RaftState            string   `protobuf:"bytes,9,opt,name=raft_state,json=raftState,proto3" json:"raft_state,omitempty"`
	XXX_NoUnkeyedLiteral struct{} `json:"-"`
	XXX_unrecognized     []byte   `json:"-"`
	XXX_sizecache        int32    `json:"-"`
}

func (m *PeerInfo) Reset()         { *m = PeerInfo{} }
func (m *PeerInfo) String() string { return proto.CompactTextString(m) }
func (*PeerInfo) ProtoMessage()    {}
func (*PeerInfo) Descriptor() ([]byte, []int) {
	return fileDescriptor_555bd8c177793206, []int{6}
}

func (m *PeerInfo) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_PeerInfo.Unmarshal(m, b)
}
func (m *PeerInfo) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_PeerInfo.Marshal(b, m, deterministic)
}
func (m *PeerInfo) XXX_Merge(src proto.Message) {
	xxx_messageInfo_PeerInfo.Merge(m, src)
}
func (m *PeerInfo) XXX_Size() int {
	return xxx_messageInfo_PeerInfo.Size(m)
}
func (m *PeerInfo) XXX_DiscardUnknown() {
	xxx_messageInfo_PeerInfo.DiscardUnknown(m)
}

var xxx_messageInfo_PeerInfo proto.InternalMessageInfo

func (m *PeerInfo) GetId() string {
	if m != nil {
		return m.Id
	}
	return ""
}

func (m *PeerInfo) GetRemoteAddress() string {
	if m != nil {
		return m.RemoteAddress
	}
	return ""
}

func (m *PeerInfo) GetHostname() string {
	if m != nil {
		return m.Hostname
	}
	return ""
}

func (m *PeerInfo) GetBroadcastAddress() string {
	if m != nil {
		return m.BroadcastAddress
	}
	return ""
}

func (m *PeerInfo) GetTcpPort() int32 {
	if m != nil {
		return m.TcpPort
	}
	return 0
}

func (m *PeerInfo) GetHttpPort() int32 {
	if m != nil {
		return m.HttpPort
	}
	return 0
}

func (m *PeerInfo) GetVersion() string {
	if m != nil {
		return m.Version
	}
	return ""
}

func (m *PeerInfo) GetLastUpdate() int64 {
	if m != nil {
		return m.LastUpdate
	}
	return 0
}

func (m *PeerInfo) GetRaftState() string {
	if m != nil {
		return m.RaftState
	}
	return ""
}

func init() {
	proto.RegisterType((*PingRequest)(nil), "pbapi.PingRequest")
	proto.RegisterType((*PingReply)(nil), "pbapi.PingReply")
	proto.RegisterType((*IdentifyRequest)(nil), "pbapi.IdentifyRequest")
	proto.RegisterType((*IdentifyReply)(nil), "pbapi.IdentifyReply")
	proto.RegisterType((*Channel)(nil), "pbapi.Channel")
	proto.RegisterType((*Topic)(nil), "pbapi.Topic")
	proto.RegisterType((*PeerInfo)(nil), "pbapi.PeerInfo")
}

func init() {
	proto.RegisterFile("common.proto", fileDescriptor_555bd8c177793206)
}

var fileDescriptor_555bd8c177793206 = []byte{
	// 530 bytes of a gzipped FileDescriptorProto
	0x1f, 0x8b, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00, 0x02, 0xff, 0x94, 0x94, 0xdf, 0x8a, 0xd3, 0x40,
	0x14, 0xc6, 0x49, 0x93, 0xb4, 0xc9, 0xe9, 0xb6, 0xbb, 0xce, 0x85, 0xc4, 0x7f, 0x58, 0xb2, 0x08,
	0x45, 0xa1, 0x42, 0x05, 0xef, 0x75, 0x41, 0xd8, 0x1b, 0x29, 0x51, 0xaf, 0xc3, 0x34, 0x39, 0x4d,
	0x03, 0x49, 0x66, 0x9c, 0x99, 0x88, 0x7d, 0x17, 0x1f, 0xc5, 0xc7, 0xf1, 0x39, 0x44, 0x66, 0x26,
	0xcd, 0x6e, 0x6b, 0x2b, 0x78, 0x37, 0xf3, 0x3b, 0xdf, 0xf4, 0xcc, 0x37, 0xfd, 0x4e, 0xe0, 0x22,
	0x63, 0x75, 0xcd, 0x9a, 0x05, 0x17, 0x4c, 0x31, 0xe2, 0xf3, 0x35, 0xe5, 0x65, 0xbc, 0x84, 0xf1,
	0xaa, 0x6c, 0x8a, 0x04, 0xbf, 0xb6, 0x28, 0x15, 0xb9, 0x06, 0x8f, 0x23, 0x8a, 0xc8, 0x99, 0x39,
	0xf3, 0xf1, 0xf2, 0x72, 0x61, 0x44, 0x8b, 0x15, 0xa2, 0xb8, 0x6d, 0x36, 0x2c, 0x31, 0xc5, 0xf8,
	0x35, 0x84, 0xf6, 0x0c, 0xaf, 0x76, 0xe4, 0x0a, 0x5c, 0x14, 0xf6, 0x40, 0x98, 0xe8, 0xa5, 0x26,
	0xb5, 0x2c, 0xa2, 0x81, 0x25, 0xb5, 0x2c, 0xe2, 0xb7, 0x70, 0x79, 0x9b, 0x63, 0xa3, 0xca, 0xcd,
	0xee, 0xbf, 0x1a, 0xfd, 0x74, 0x60, 0x72, 0x77, 0xf0, 0x74, 0xb7, 0x47, 0x10, 0xa8, 0x8c, 0xa7,
	0x9c, 0x09, 0x65, 0x5a, 0xfa, 0xc9, 0x48, 0x65, 0x7c, 0xc5, 0x84, 0x22, 0x4f, 0x20, 0xdc, 0x2a,
	0xd5, 0xd5, 0x5c, 0x53, 0x0b, 0x34, 0x30, 0xc5, 0x08, 0x46, 0xdf, 0x50, 0xc8, 0x92, 0x35, 0x91,
	0x67, 0x7e, 0x6d, 0xbf, 0x25, 0xaf, 0xe0, 0xc1, 0x5a, 0x30, 0x9a, 0x67, 0x54, 0xaa, 0x94, 0xe6,
	0xb9, 0x40, 0x29, 0x23, 0xdf, 0x68, 0xae, 0xfa, 0xc2, 0x3b, 0xcb, 0xc9, 0x63, 0x08, 0xb6, 0x4c,
	0xaa, 0x86, 0xd6, 0x18, 0x0d, 0x8d, 0xa6, 0xdf, 0xc7, 0xbf, 0x1d, 0x18, 0xdd, 0x6c, 0x69, 0xd3,
	0x60, 0x45, 0xae, 0x61, 0x22, 0xb4, 0xf5, 0x16, 0xd3, 0x8c, 0xb5, 0x8d, 0x32, 0x16, 0xbc, 0xe4,
	0xa2, 0x83, 0x37, 0x9a, 0x69, 0x51, 0x8d, 0x52, 0xd2, 0x62, 0x2f, 0x1a, 0x58, 0x51, 0x07, 0x7b,
	0x91, 0x2a, 0x6b, 0x64, 0xad, 0xea, 0x44, 0xae, 0x15, 0x75, 0xd0, 0x8a, 0x9e, 0x01, 0x28, 0xc6,
	0xcb, 0x2c, 0x35, 0x17, 0xb3, 0x06, 0x43, 0x43, 0x3e, 0xd2, 0x1a, 0x09, 0x01, 0xcf, 0x14, 0xac,
	0x2b, 0xb3, 0xd6, 0xaf, 0x85, 0xdf, 0x4b, 0x95, 0x6e, 0x2a, 0x5a, 0x18, 0x2b, 0x7e, 0x12, 0x68,
	0xf0, 0xa1, 0xa2, 0x05, 0x79, 0x08, 0x43, 0x4e, 0x5b, 0x89, 0x79, 0x34, 0x32, 0x95, 0x6e, 0x47,
	0x9e, 0x42, 0x88, 0x7c, 0x8b, 0x35, 0x0a, 0x5a, 0x45, 0xc1, 0xcc, 0x99, 0x07, 0xc9, 0x1d, 0x88,
	0x7f, 0x39, 0xe0, 0x7f, 0xd6, 0x4d, 0xff, 0x76, 0xe6, 0x9c, 0x76, 0xb6, 0x17, 0xad, 0x77, 0x0a,
	0xe5, 0x91, 0xfd, 0xf7, 0x9a, 0xf5, 0x57, 0x77, 0xef, 0x5d, 0xfd, 0x25, 0x04, 0x99, 0x7d, 0x67,
	0x19, 0x79, 0x33, 0x77, 0x3e, 0x5e, 0x4e, 0xbb, 0x40, 0x75, 0xcf, 0x9f, 0xf4, 0xf5, 0x43, 0x9b,
	0xfe, 0x91, 0xcd, 0x03, 0x3b, 0xc3, 0x23, 0x3b, 0xe7, 0x1e, 0x21, 0xfe, 0x31, 0x80, 0x60, 0x9f,
	0x5c, 0x32, 0x85, 0x41, 0x99, 0x77, 0x01, 0x1d, 0x94, 0x39, 0x79, 0x01, 0x53, 0x81, 0x35, 0x53,
	0xd8, 0x47, 0xc9, 0x0e, 0xc6, 0xc4, 0xd2, 0x53, 0x39, 0x72, 0x0f, 0x73, 0x74, 0x3a, 0x90, 0xde,
	0x99, 0x40, 0xde, 0x9f, 0x07, 0xff, 0x1f, 0xf3, 0x30, 0x3c, 0x3f, 0x0f, 0xa3, 0xc3, 0x79, 0x78,
	0x0e, 0xe3, 0x4a, 0x77, 0x6e, 0x79, 0x4e, 0x15, 0x9a, 0x7f, 0xd9, 0x4d, 0x40, 0xa3, 0x2f, 0x86,
	0xe8, 0xb0, 0x09, 0xba, 0x51, 0xa9, 0x54, 0xba, 0x1e, 0xda, 0xb0, 0x69, 0xf2, 0x49, 0x83, 0xf5,
	0xd0, 0x7c, 0x70, 0xde, 0xfc, 0x09, 0x00, 0x00, 0xff, 0xff, 0x3f, 0xaa, 0xe2, 0xfd, 0x80, 0x04,
	0x00, 0x00,
}
