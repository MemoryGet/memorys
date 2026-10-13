package modulewire

import "github.com/MemoryGet/memorys/pkg/contracts"

type blobPutInput struct {
	ExpectedSHA256            contracts.Digest
	Size                      int64
	MediaType, IdempotencyKey string
}
type blobReadInput struct{ Blob contracts.BlobRef }

func init() {
	register(invocation[blobPutInput]{}, "Context:context Input:input")
	register(blobPutInput{}, "ExpectedSHA256:expected_sha256 Size:size MediaType:media_type IdempotencyKey:idempotency_key")
	register(invocation[blobReadInput]{}, "Context:context Input:input")
	register(blobReadInput{}, "Blob:blob")
	register(response[contracts.BlobRef]{}, "RequestID:request_id InvocationID:invocation_id Result:result")
}

// EncodeBlobPutMetadata encodes only the JSON multipart metadata, never bytes or credentials.
func EncodeBlobPutMetadata(call Call, request contracts.BlobWriteRequest) ([]byte, error) {
	if request.Size < 0 || !digestValue(request.ExpectedSHA256) || request.MediaType == "" || request.IdempotencyKey == "" {
		return nil, invalid()
	}
	return encodeInvocation(call, request.Request, request.Scope, blobPutInput{request.ExpectedSHA256, request.Size, request.MediaType, request.IdempotencyKey})
}
func DecodeBlobPutMetadata(data []byte) (Call, contracts.BlobWriteRequest, error) {
	context, input, err := decodeInvocation[blobPutInput](data)
	if err != nil {
		return Call{}, contracts.BlobWriteRequest{}, err
	}
	request := contracts.BlobWriteRequest{Request: requestContext(context), Scope: context.Scope, ExpectedSHA256: input.ExpectedSHA256, Size: input.Size, MediaType: input.MediaType, IdempotencyKey: input.IdempotencyKey}
	if request.Size < 0 || !digestValue(request.ExpectedSHA256) || request.MediaType == "" || request.IdempotencyKey == "" {
		return Call{}, contracts.BlobWriteRequest{}, invalid()
	}
	return Call{context.InvocationID, context.ModuleInstanceID}, request, nil
}

// EncodeBlobReadRequest is the shared open/delete DTO; a verified grant must bind the actual operation path.
func EncodeBlobReadRequest(call Call, request contracts.BlobReadRequest) ([]byte, error) {
	if !validBlob(request.Blob) {
		return nil, invalid()
	}
	return encodeInvocation(call, request.Request, request.Scope, blobReadInput{request.Blob})
}
func DecodeBlobReadRequest(data []byte) (Call, contracts.BlobReadRequest, error) {
	context, input, err := decodeInvocation[blobReadInput](data)
	if err != nil {
		return Call{}, contracts.BlobReadRequest{}, err
	}
	if !validBlob(input.Blob) {
		return Call{}, contracts.BlobReadRequest{}, invalid()
	}
	return Call{context.InvocationID, context.ModuleInstanceID}, contracts.BlobReadRequest{Request: requestContext(context), Scope: context.Scope, Blob: input.Blob}, nil
}
func EncodeBlobPutResult(requestID, invocationID contracts.ID, blob contracts.BlobRef) ([]byte, error) {
	if !validBlob(blob) {
		return nil, invalid()
	}
	return encodeResponse(requestID, invocationID, blob)
}
func DecodeBlobPutResult(data []byte, requestID, invocationID contracts.ID, expected contracts.BlobWriteRequest) (contracts.BlobRef, error) {
	if expected.Request.RequestID != requestID {
		return contracts.BlobRef{}, invalid()
	}
	blob, err := decodeResponse[contracts.BlobRef](data, requestID, invocationID)
	if err != nil {
		return contracts.BlobRef{}, err
	}
	if !validBlob(blob) || blob.SHA256 != expected.ExpectedSHA256 || blob.Size != expected.Size || blob.MediaType != expected.MediaType {
		return contracts.BlobRef{}, invalid()
	}
	return blob, nil
}
func EncodeBlobDeleteAck(requestID, invocationID contracts.ID) ([]byte, error) {
	return EncodeIndexAck(requestID, invocationID)
}
func DecodeBlobDeleteAck(data []byte, requestID, invocationID contracts.ID) error {
	return DecodeIndexAck(data, requestID, invocationID)
}
func validBlob(blob contracts.BlobRef) bool {
	return blob.ID != "" && digestValue(blob.SHA256) && blob.Size >= 0 && blob.MediaType != ""
}
