package main

import (
	"encoding/json"
	"io"

	"github.com/MemoryGet/memorys/pkg/modulewire"
)

func isBlobOperation(operation string) bool {
	return operation == "blob_store_put" || operation == "blob_store_open" || operation == "blob_store_delete"
}

// inspectBlob round-trips DTO metadata only; no multipart bytes or network are executed.
func inspectBlob(output io.Writer, operation string, request, response json.RawMessage) error {
	result := map[string]json.RawMessage{}
	if operation == "blob_store_put" {
		call, value, err := modulewire.DecodeBlobPutMetadata(request)
		if err != nil {
			return err
		}
		blob, err := modulewire.DecodeBlobPutResult(response, value.Request.RequestID, call.InvocationID, value)
		if err != nil {
			return err
		}
		result["request"], err = modulewire.EncodeBlobPutMetadata(call, value)
		if err != nil {
			return err
		}
		result["response"], err = modulewire.EncodeBlobPutResult(value.Request.RequestID, call.InvocationID, blob)
		if err != nil {
			return err
		}
	} else {
		call, value, err := modulewire.DecodeBlobReadRequest(request)
		if err != nil {
			return err
		}
		result["request"], err = modulewire.EncodeBlobReadRequest(call, value)
		if err != nil {
			return err
		}
		if operation == "blob_store_delete" {
			if err := modulewire.DecodeBlobDeleteAck(response, value.Request.RequestID, call.InvocationID); err != nil {
				return err
			}
			result["response"], err = modulewire.EncodeBlobDeleteAck(value.Request.RequestID, call.InvocationID)
			if err != nil {
				return err
			}
		}
	}
	return json.NewEncoder(output).Encode(result)
}
