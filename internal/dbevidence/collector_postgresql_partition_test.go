package dbevidence

import "testing"

func midLevelPartitionTable() *TableEvidence {
	return &TableEvidence{ObjectRef: "database://primary/public/events_2025",
		Partition: &Partition{Partitioned: true, Method: "hash", Expression: "region", ChildObjects: []string{}}}
}

// 多级分区: 中间层表既是分区又是父表。登记它所属的父分区不得抹掉它自身的
// 分区父表事实,否则规范证据会丢掉分区方法并且随行序变化。
func TestAttachPartitionParentKeepsMidLevelParentFacts(t *testing.T) {
	table := midLevelPartitionTable()
	attachPartitionParent(table, "database://primary/public/events", "FOR VALUES FROM ('2025-01-01') TO ('2026-01-01')")
	if !table.Partition.Partitioned || table.Partition.Method != "hash" || table.Partition.Expression != "region" {
		t.Fatalf("中间层表的分区父表事实被覆盖: %#v", table.Partition)
	}
	if table.Partition.ParentObject != "database://primary/public/events" || table.Partition.Bound == "" {
		t.Fatalf("未记录所属父分区: %#v", table.Partition)
	}
}

// 两种行序必须产出同一份证据 —— 否则同一套 schema 会哈希出两份规范证据。
func TestPartitionLinkingIsRowOrderIndependent(t *testing.T) {
	childFirst := midLevelPartitionTable()
	appendPartitionChild(childFirst, "database://primary/public/events_2025_a")
	attachPartitionParent(childFirst, "database://primary/public/events", "FOR VALUES IN (2025)")

	parentFirst := midLevelPartitionTable()
	attachPartitionParent(parentFirst, "database://primary/public/events", "FOR VALUES IN (2025)")
	appendPartitionChild(parentFirst, "database://primary/public/events_2025_a")

	if childFirst.Partition.Partitioned != parentFirst.Partition.Partitioned ||
		childFirst.Partition.Method != parentFirst.Partition.Method ||
		childFirst.Partition.Expression != parentFirst.Partition.Expression ||
		childFirst.Partition.ParentObject != parentFirst.Partition.ParentObject ||
		childFirst.Partition.Bound != parentFirst.Partition.Bound ||
		len(childFirst.Partition.ChildObjects) != len(parentFirst.Partition.ChildObjects) {
		t.Fatalf("行序改变了分区证据: childFirst=%#v parentFirst=%#v", childFirst.Partition, parentFirst.Partition)
	}
	if len(childFirst.Partition.ChildObjects) != 1 || !childFirst.Partition.Partitioned ||
		childFirst.Partition.Method != "hash" {
		t.Fatalf("子表先到时中间层事实不完整: %#v", childFirst.Partition)
	}
}

// 叶子分区此前没有分区结构,登记后应保持"非分区父表"。
func TestAttachPartitionParentOnLeafStaysNonPartitioned(t *testing.T) {
	leaf := &TableEvidence{ObjectRef: "database://primary/public/events_2025_a"}
	attachPartitionParent(leaf, "database://primary/public/events_2025", "FOR VALUES WITH (modulus 4, remainder 0)")
	if leaf.Partition == nil || leaf.Partition.Partitioned || leaf.Partition.Method != "" {
		t.Fatalf("叶子分区不应被记成分区父表: %#v", leaf.Partition)
	}
	if leaf.Partition.ParentObject == "" || leaf.Partition.Bound == "" {
		t.Fatalf("叶子分区缺少父表绑定: %#v", leaf.Partition)
	}
}

// 父表尚未被分区父表阶段命中时,登记子表要把它建立成分区父表。
func TestAppendPartitionChildEstablishesParent(t *testing.T) {
	parent := &TableEvidence{ObjectRef: "database://primary/public/events"}
	appendPartitionChild(parent, "database://primary/public/events_2025")
	if parent.Partition == nil || !parent.Partition.Partitioned || len(parent.Partition.ChildObjects) != 1 {
		t.Fatalf("父表未被建立为分区父表: %#v", parent.Partition)
	}
}
