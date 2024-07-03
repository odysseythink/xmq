# to_xmq

A tool for publishing to an xmq topic with data from `stdin`.

## Usage

```
Usage of ./to_xmq:
  -delimiter string
    	character to split input from stdin (default "\n")
  -xmqd-tcp-address value
    	destination xmqd TCP address (may be given multiple times)
  -producer-opt value
    	option to passthrough to xmq.Producer (may be given multiple times)
  -rate int
    	Throttle messages to n/second. 0 to disable
  -topic string
    	XMQ topic to publish to
```
    
### Examples

Publish each line of a file:

```bash
$ cat source.txt | to_xmq -topic="topic" -xmqd-tcp-address="127.0.0.1:4150"
```

Publish three messages, in one go:

```bash
$ echo "one,two,three" | to_xmq -delimiter="," -topic="topic" -xmqd-tcp-address="127.0.0.1:4150"
```
