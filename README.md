# GOBBLER Overview

GOBBLER is a log ingestion REST service which offers the following functionality
- it accepts JSON descriptions of the structure of ingested items (their types)
- it accepts arrays of JSON objects representing the defined items, and validates them for consistency
- it converts the JSON representation of well-formed items to CSV strings (assuming fixes sequence of items fields in a type)
- it then stores CSV stings representing items of the same type in 
    - time-and-type-stamped sequence of local files, or
    - time-and-type-stamped sequence of AZURE blobs.

The result is a collection of files or blobs with CSV strings representing instances of the defined log item types. 

Log items are named sequences of fields/columns of the following types
- bool
- datetime
- dynamic
- int
- real
- string, and 
- timespan.
  
A field can be declared as optional or required, and can have a default value. The interaction between these
two properties is as follows
- if a field is required AND has a default value, then if the field is not assigned a value on input, the default value is used
- if a filed is optional the default value is ignored, even if it is provided.

The value of **dynamic** field type should be a well-formed JSON object, which is a common approach of passing 
complex values to analytic engines like Kusto (aka Azure Analytics). GOBBLER does not understand the structure
of dynamic fields, but it validates that they contain valid JSON objects. 

In addition to the user-defined fields GOBBLER adds, as the first field, a time stamps. The field is called
**timestamp** and it contains datetime of when the item was processed/validated by GOBBLER. The name **timestamp** 
is therefore reserved.  

GOBBLER buffers items of the same type before flushing/appending them them to the current/active log file/blob. The number
of items in the buffer is the same for all item types and can be set via service interface. A log file/blob contains items
of only one type and its name is a concatenation of the timestamp when the file/blob was created and the item type name. 

While a log file/blog is open for receiving new items it should not be used for analysis (opening it in a spreadsheet or uploading
to columnar store like Kusto). The property **latencyMinutes** of item definition declares after what time, since creation of the
current file/blob, GOBBLER should rotate it (close the current one and open a new one with a new start time). GOBBLER provides 
a function to force rotation of a current file/blob if it is needed for analysis sooner than its latency would imply.  

GOBBLER puts all files/blobs with items of the same type in one folder (directory for local file system, or container
for Azure blobs). The name of the folder can be provided in item definition, but if omitted it will be the same
as the item type name. If multiple definitions provide the same folder name, the folder will contain files
with different item types distinguishable by their names. 

# GOBBLER Architecture

Gobble architecture has four distinct parts
- the item definition part (`items` module)
- the ingestion pipeline (`pipeline` module) 
- the writers (`writers` module), and 
- the REST interface (`server` module)

## Item definitions

The structure of item type is given to Gobbler as a JSON object of the following schema

``` JSON
{
  "title": "itemTypeSchema",
  "description": "item (log entry) type description",
  "type": "object",
  "properties": {
    "name": {"type": "string"},
    "documentation": {"type": "string", "optional": true},
    "folder": {"type": "string", "minLength": 3, "optional": true},
    "latencyMinutes": {"type": "integer", "optional": true},
    "orderedColumns": {
      "type": "array",
      "items": {  
        "type": "object",
        "properties": {
          "name": { "type": "string"},  
          "type": { "type": "string"},
          "optional": { "type": "boolean", "optional": true},
          "defaultValue": { "type": ["string", "number", "boolean"], "optional": true}
        }
      },
      "uniqueItems": true,
      "minItems": 1
    }
  }
}
```

where 
- `name` is the item type name
- `folder` is the name of the directory, or the blob storage container, in which files, or blobs, with ingested items of the named type will be stored
- `latencyMinutes` is the time where items file, or blob, will be rotated
- `columns` is order list of item fields, including information about field name, type, optionality, and default value. 

Gobbler has an REST interface for introducing items types, one at a time. In other words when Gobbler is started it cannot ingest any items until it is provided with their definitions.    

## The pipeline

Gobbler's exposes REST interface for ingesting log item represented as arrays of JSON records. Items are ingested through a multi‑stage pipeline with type-aware dispatcher, queues and writers. The stages are:

**Item Parsing & Validation**
  - Invalid JSON records are rejected immediately
  - Valid records proceed to the next stage
**Conversion to CSV**
  - Produces a compact, normalized representation of items as CSV strings, with fields corresponding to item types  
  - The sting is put into a struct with item type and placed in central input queue
**Central Dispatcher**
  - Reads from the input queue
  - Determines item type
  - Routes each CSV string into the appropriate writer's queue.

## The writers

Each item type has worker with a queue handling items of that type. A worker is composed of a batcher followed by a writer.
  - Batcher accumulated CSV strings in a buffer
  - When the buffer reaches its defined size, it is passed to the Writer
  - Writer appended the CSV items to a file/blob in the corresponding directory/container
  - When a file/blob reaches a configured size or time limit, the writer rotates it

This is the end of the ingestion pipeline.

## The Rest interface 

Gobbler exposes three REST endpoints:  

- gobbler/definition -> to adds new item definition
- gobbler/ingest -> to ingests and saves an array of JSON item objects
- gobbler/manage/... -> to execute Gobbler server management commands (stats, status, shot down, ...) 

More detailed outline of the Gobbler architecture is in `docs\pipeline.md` document. 




