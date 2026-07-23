// package radixipv1

// import (
// 	"context"

// 	"google.golang.org/grpc"
// )

// // Metadata message definition
// type Metadata struct {
// 	Value      string            `protobuf:"bytes,1,opt,name=value,proto3" json:"value,omitempty"`
// 	Attributes map[string]string `protobuf:"bytes,2,rep,name=attributes,proto3" json:"attributes,omitempty" protobuf_key:"bytes,1,opt,name=key,proto3" protobuf_val:"bytes,2,opt,name=value,proto3"`
// }

// type InsertRequest struct {
// 	Prefix   string    `protobuf:"bytes,1,opt,name=prefix,proto3" json:"prefix,omitempty"`
// 	Metadata *Metadata `protobuf:"bytes,2,opt,name=metadata,proto3" json:"metadata,omitempty"`
// }

// type InsertResponse struct {
// 	Success      bool   `protobuf:"varint,1,opt,name=success,proto3" json:"success,omitempty"`
// 	IsNew        bool   `protobuf:"varint,2,opt,name=is_new,json=isNew,proto3" json:"is_new,omitempty"`
// 	ErrorMessage string `protobuf:"bytes,3,opt,name=error_message,json=errorMessage,proto3" json:"error_message,omitempty"`
// }

// type LookupRequest struct {
// 	Ip string `protobuf:"bytes,1,opt,name=ip,proto3" json:"ip,omitempty"`
// }

// type LookupResponse struct {
// 	Found    bool      `protobuf:"varint,1,opt,name=found,proto3" json:"found,omitempty"`
// 	Metadata *Metadata `protobuf:"bytes,2,opt,name=metadata,proto3" json:"metadata,omitempty"`
// }

// type RemoveRequest struct {
// 	Prefix string `protobuf:"bytes,1,opt,name=prefix,proto3" json:"prefix,omitempty"`
// }

// type RemoveResponse struct {
// 	Found    bool      `protobuf:"varint,1,opt,name=found,proto3" json:"found,omitempty"`
// 	Metadata *Metadata `protobuf:"bytes,2,opt,name=metadata,proto3" json:"metadata,omitempty"`
// }

// type ContainsRequest struct {
// 	Prefix string `protobuf:"bytes,1,opt,name=prefix,proto3" json:"prefix,omitempty"`
// }

// type ContainsResponse struct {
// 	Contains bool `protobuf:"varint,1,opt,name=contains,proto3" json:"contains,omitempty"`
// }

// type ClearRequest struct{}

// type ClearResponse struct {
// 	Success bool `protobuf:"varint,1,opt,name=success,proto3" json:"success,omitempty"`
// }

// type StatsRequest struct{}

// type StatsResponse struct {
// 	Inserts  int64 `protobuf:"varint,1,opt,name=inserts,proto3" json:"inserts,omitempty"`
// 	Lookups  int64 `protobuf:"varint,2,opt,name=lookups,proto3" json:"lookups,omitempty"`
// 	Hits     int64 `protobuf:"varint,3,opt,name=hits,proto3" json:"hits,omitempty"`
// 	Misses   int64 `protobuf:"varint,4,opt,name=misses,proto3" json:"misses,omitempty"`
// 	Removals int64 `protobuf:"varint,5,opt,name=removals,proto3" json:"removals,omitempty"`
// 	Size     int64 `protobuf:"varint,6,opt,name=size,proto3" json:"size,omitempty"`
// }

// type StreamInsertResponse struct {
// 	InsertedCount uint64 `protobuf:"varint,1,opt,name=inserted_count,json=insertedCount,proto3" json:"inserted_count,omitempty"`
// }

// // RadixServiceServer is the server API for RadixService service.
// type RadixServiceServer interface {
// 	Insert(context.Context, *InsertRequest) (*InsertResponse, error)
// 	Lookup(context.Context, *LookupRequest) (*LookupResponse, error)
// 	Remove(context.Context, *RemoveRequest) (*RemoveResponse, error)
// 	Contains(context.Context, *ContainsRequest) (*ContainsResponse, error)
// 	Clear(context.Context, *ClearRequest) (*ClearResponse, error)
// 	GetStats(context.Context, *StatsRequest) (*StatsResponse, error)
// 	StreamInsert(RadixService_StreamInsertServer) error
// }

// type RadixService_StreamInsertServer interface {
// 	Recv() (*InsertRequest, error)
// 	SendAndClose(*StreamInsertResponse) error
// 	grpc.ServerStream
// }

// func RegisterRadixServiceServer(s grpc.ServiceRegistrar, srv RadixServiceServer) {
// 	s.RegisterService(&RadixService_ServiceDesc, srv)
// }

// var RadixService_ServiceDesc = grpc.ServiceDesc{
// 	ServiceName: "radixip.v1.RadixService",
// 	HandlerType: (*RadixServiceServer)(nil),
// 	Methods: []grpc.MethodDesc{
// 		{
// 			MethodName: "Insert",
// 			Handler:    _RadixService_Insert_Handler,
// 		},
// 		{
// 			MethodName: "Lookup",
// 			Handler:    _RadixService_Lookup_Handler,
// 		},
// 		{
// 			MethodName: "Remove",
// 			Handler:    _RadixService_Remove_Handler,
// 		},
// 		{
// 			MethodName: "Contains",
// 			Handler:    _RadixService_Contains_Handler,
// 		},
// 		{
// 			MethodName: "Clear",
// 			Handler:    _RadixService_Clear_Handler,
// 		},
// 		{
// 			MethodName: "GetStats",
// 			Handler:    _RadixService_GetStats_Handler,
// 		},
// 	},
// 	Streams: []grpc.StreamDesc{
// 		{
// 			StreamName:    "StreamInsert",
// 			Handler:       _RadixService_StreamInsert_Handler,
// 			ClientStreams: true,
// 		},
// 	},
// 	Metadata: "proto/radixip/v1/radixip.proto",
// }

// func _RadixService_Insert_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
// 	in := new(InsertRequest)
// 	if err := dec(in); err != nil {
// 		return nil, err
// 	}
// 	if interceptor == nil {
// 		return srv.(RadixServiceServer).Insert(ctx, in)
// 	}
// 	info := &grpc.UnaryServerInfo{
// 		Server:     srv,
// 		FullMethod: "/radixip.v1.RadixService/Insert",
// 	}
// 	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
// 		return srv.(RadixServiceServer).Insert(ctx, req.(*InsertRequest))
// 	}
// 	return interceptor(ctx, in, info, handler)
// }

// func _RadixService_Lookup_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
// 	in := new(LookupRequest)
// 	if err := dec(in); err != nil {
// 		return nil, err
// 	}
// 	if interceptor == nil {
// 		return srv.(RadixServiceServer).Lookup(ctx, in)
// 	}
// 	info := &grpc.UnaryServerInfo{
// 		Server:     srv,
// 		FullMethod: "/radixip.v1.RadixService/Lookup",
// 	}
// 	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
// 		return srv.(RadixServiceServer).Lookup(ctx, req.(*LookupRequest))
// 	}
// 	return interceptor(ctx, in, info, handler)
// }

// func _RadixService_Remove_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
// 	in := new(RemoveRequest)
// 	if err := dec(in); err != nil {
// 		return nil, err
// 	}
// 	if interceptor == nil {
// 		return srv.(RadixServiceServer).Remove(ctx, in)
// 	}
// 	info := &grpc.UnaryServerInfo{
// 		Server:     srv,
// 		FullMethod: "/radixip.v1.RadixService/Remove",
// 	}
// 	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
// 		return srv.(RadixServiceServer).Remove(ctx, req.(*RemoveRequest))
// 	}
// 	return interceptor(ctx, in, info, handler)
// }

// func _RadixService_Contains_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
// 	in := new(ContainsRequest)
// 	if err := dec(in); err != nil {
// 		return nil, err
// 	}
// 	if interceptor == nil {
// 		return srv.(RadixServiceServer).Contains(ctx, in)
// 	}
// 	info := &grpc.UnaryServerInfo{
// 		Server:     srv,
// 		FullMethod: "/radixip.v1.RadixService/Contains",
// 	}
// 	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
// 		return srv.(RadixServiceServer).Contains(ctx, req.(*ContainsRequest))
// 	}
// 	return interceptor(ctx, in, info, handler)
// }

// func _RadixService_Clear_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
// 	in := new(ClearRequest)
// 	if err := dec(in); err != nil {
// 		return nil, err
// 	}
// 	if interceptor == nil {
// 		return srv.(RadixServiceServer).Clear(ctx, in)
// 	}
// 	info := &grpc.UnaryServerInfo{
// 		Server:     srv,
// 		FullMethod: "/radixip.v1.RadixService/Clear",
// 	}
// 	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
// 		return srv.(RadixServiceServer).Clear(ctx, req.(*ClearRequest))
// 	}
// 	return interceptor(ctx, in, info, handler)
// }

// func _RadixService_GetStats_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
// 	in := new(StatsRequest)
// 	if err := dec(in); err != nil {
// 		return nil, err
// 	}
// 	if interceptor == nil {
// 		return srv.(RadixServiceServer).GetStats(ctx, in)
// 	}
// 	info := &grpc.UnaryServerInfo{
// 		Server:     srv,
// 		FullMethod: "/radixip.v1.RadixService/GetStats",
// 	}
// 	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
// 		return srv.(RadixServiceServer).GetStats(ctx, req.(*StatsRequest))
// 	}
// 	return interceptor(ctx, in, info, handler)
// }

// func _RadixService_StreamInsert_Handler(srv interface{}, stream grpc.ServerStream) error {
// 	return srv.(RadixServiceServer).StreamInsert(&radixServiceStreamInsertServer{stream})
// }

// type radixServiceStreamInsertServer struct {
// 	grpc.ServerStream
// }

// func (x *radixServiceStreamInsertServer) SendAndClose(m *StreamInsertResponse) error {
// 	return x.ServerStream.SendMsg(m)
// }

// func (x *radixServiceStreamInsertServer) Recv() (*InsertRequest, error) {
// 	m := new(InsertRequest)
// 	if err := x.ServerStream.RecvMsg(m); err != nil {
// 		return nil, err
// 	}
// 	return m, nil
// }
