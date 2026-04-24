# Storage and REST APIs 

The document describes how Gobbler stores ingested items and the Gobbler REST API. 

## Storage modes

Gobbler works in one of two storage modes
- file mode, and
- blob mode. 

In file mode **all** writes are of the FileWriter kind, and write items to files in subdirectories of a designated output directory (outputDir) of the server's file system. Each item type definition contains a field `folder`. The value of that field, if provided, is used as the name of the item-type-specific subdirectory of the outputDir used by the FileWriter associated with that item type. If `folder` value is omitted, the name of the item type is used. 

In the blob mode **all** writers are of the BlobWriter kind, and write items to blobs in type-specific containers. Similarly to file mode, value of that `folder` field of item type definition is used as the name of the item-type-specific container used by the BlobWriter associated with that item type. If `folder` value is omitted, the name of the item type is used.

The decision to use either file or blob mode for all writers, and not to mix them, is bases on the assumption that managing Gobbler instances that write to both files and blobs would get over complicated. It should be much simpler to manages multiple instances of Gobbler, each of them working in single storage mode.   

To select the mode Gobbler provides REST endpoint `gobbler/pipeline/configure`. The endpoint accepts JSON object of the following schema
``` JSON
{
  "title": "gobblerStorageModeSchema",
  "description": "describes the storage mode of gobbler instance",
  "type": "object",
  "properties": {
    "mode": {"type": "string", "optional": false},
    "outputDir": {"type": "string", "optional": true},
    "accountName": {"type": "string", "optional": true},
    "accountKey": {"type": "string", "optional": true},
    "centralQueueSize": {"type": "integer", "optional": false},
    "workerQueueSize": {"type": "integer", "optional": false},
    "batchSize": {"type": "integer", "optional": false}
  }
}
```

The value of `mode` must be either "file" or "blob". If the mode is "file" the `outputDir` must be provided. If the mode is "blob", both `accountName` and `accountKey` must be provides. 

The values of `outputDir` is passed to FileWrites. The values of `accountName` and `accountKey` are passed to BlobWriters. The value of `batchSize` defines the batchSize property of all writers. 

The `gobbler/pipeline/configure` endpoint should be called before any other calls, as it defines global configuration constraints of the pipeline. 


## Gobbler REST API

Gobbler's REST API is implemented using Chi router following the examples at https://github.com/go-chi/chi/tree/master/_examples/rest.

The routs are divided into groups, indicated by the second element of the route
- `definition` group
- `pipeline` group, and 
- `ingest` group

Gobbler endpoints all accept and return JSON.  

**definition** group

- `gobbler/definition/add` ->  takes item definition JSON object as input. It does different things depending on if the pipeline has been started or not
  - if the pipeline has not been started it parses, validates and adds the definition to DefinitionList 
  - if the pipeline is running it (1) parses, validates and adds the definition to DefinitionListitem (2) creates and starts the new type's writer (3) creates a new worker associate with the writer (4) creates new TypeDescriptor and (5) adds the new type to the pipeline (pipeline.AddItemType())
  
- `gobbler/definition/list` -> lists current item definitions
  
- `gobbler/definition/remove` -> removes existing item definition. If the pipeline has been started, the operation stops the corresponding writer, which flushes the current file before closing it, removes the writer and the corresponding worker, removes the type from the routing table, and finally removes the definition.

**pipeline** group:

- `gobbler/pipeline/configure` -> sets pipeline mode, rootDirectory or blob storage credentials, queues sizes, and writers' batch size
  
``` go
// pipeline/config.go
type StorageMode string

const (
    StorageModeFile StorageMode = "file"
    StorageModeBlob StorageMode = "blob"
)

type Config struct {
    Mode             StorageMode
    OutputDir        string      // file mode only
    AccountName      string      // blob mode only
    AccountKey       string      // blob mode only
    CentralQueueSize int
    WorkerQueueSize  int
    BatchSize        int
}
```

- `gobbler/pipeline/start` -> starts the pipeline if it has been (1) configures and (2) at least one item type definition has been added. It created central queue and dispatcher, routing table, workers and writers. 
  
- `gobbler/pipeline/stop` -> stops the dispatcher, stops all writers, which causes them to flush the buffer and close files/blobs, and resets the pipeline to the state from before it was started. After that the pipeline can be reconfigured and restarted again.  
  
- `gobbler/pipeline/rotate` -> tells specific type writer to flush, close and rotate its file/blob. used to enable access to the last/active file/blob. 
  

- `gobbler/pipeline/status` -> provides basic statistics of the pipeline (details tbd)

**ingest** group of one:

- `gobbler/ingest` -> ingest an array of items (the main function of GOBBLER)

- If a partial rout ends with "/" and no arguments, like gobbler/pipeline/, it is a request for description of the next element(s) of the rout so gobbler/pipeline/ should return information about four pipeline operations: start, stop, rotate and status.

- If a complete route, e.g. gobbler/pipeline/start/ ends with "/" and no arguments, the server should return short description of the gobbler pipeline start command, the JSON object that should be passed to it (if any), and what will be returned (based on the implementation of the command)