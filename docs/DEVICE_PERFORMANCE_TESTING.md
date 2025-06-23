# 如何测试设备（磁盘）的最大IOPS和BPS

在配置 `KubeDiskGuard` 的智能限速阈值时，首先需要了解节点上磁盘的性能基准，特别是最大IOPS（每秒输入/输出操作数）和最大BPS（每秒字节数，即带宽）。本文档将指导您如何使用业界标准的 `fio` 工具来完成这项测试。

## 1. 简介：使用 fio 工具

`fio` (Flexible I/O Tester) 是一个功能极其强大的开源IO压力测试工具。它能够模拟多种复杂的IO模式，支持多线程、多进程，并能提供详尽的性能统计数据，是磁盘性能基准测试的首选工具。

## 2. 安装 fio

`fio` 通常可以通过系统的包管理器直接安装。

**对于 Ubuntu/Debian 系统:**
```bash
sudo apt-get update
sudo apt-get install -y fio
```

**对于 CentOS/RHEL/Fedora 系统:**
```bash
sudo yum install -y fio
# 或者
sudo dnf install -y fio
```

## 3. 测试命令详解

`fio` 的参数众多，下面我们针对几种典型的场景提供测试命令。

**测试前置条件**：
- 在一个IO相对空闲的节点上执行测试，避免业务干扰。
- 在你希望测试的磁盘或文件系统上创建一个测试目录，例如 `/data`。所有测试命令将在该目录下执行。

---

### 3.1 测试最大随机读 IOPS

这是衡量磁盘处理大量小文件读取请求能力的关键指标。

```bash
fio --name=randread_iops_test \
    --directory=/data \
    --ioengine=libaio \
    --iodepth=128 \
    --rw=randread \
    --bs=4k \
    --direct=1 \
    --size=10G \
    --numjobs=16 \
    --runtime=60 \
    --group_reporting
```

**参数解释:**
- `--name`: 测试任务的名称。
- `--directory`: 测试文件存放的目录，**请确保此目录在您想测试的磁盘上**。
- `--ioengine=libaio`: 使用 Linux 的异步IO引擎（Asynchronous I/O），性能更好。
- `--iodepth=128`: IO队列深度。高队列深度可以更好地压榨磁盘性能。
- `--rw=randread`: 测试模式为随机读。
- `--bs=4k`: 块大小（Block Size）。测试IOPS通常使用小块（如4k）。
- `--direct=1`: 绕过系统Page Cache，直接访问磁盘，结果更接近真实物理性能。
- `--size=10G`: 测试文件的总大小。请确保磁盘空间足够。
- `--numjobs=16`: 并发任务数。`iodepth` * `numjobs` 代表了总的并发请求数。
- `--runtime=60`: 测试持续时间（秒）。
- `--group_reporting`: 将所有任务的报告合并，方便查看总体结果。

---

### 3.2 测试最大随机写 IOPS

衡量磁盘处理大量小文件写入请求的能力。

```bash
fio --name=randwrite_iops_test \
    --directory=/data \
    --ioengine=libaio \
    --iodepth=128 \
    --rw=randwrite \
    --bs=4k \
    --direct=1 \
    --size=10G \
    --numjobs=16 \
    --runtime=60 \
    --group_reporting
```
*参数与随机读类似，仅将 `--rw` 改为 `randwrite`。*

---

### 3.3 测试最大顺序读 BPS (带宽)

衡量磁盘读取大文件的能力。

```bash
fio --name=seqread_bps_test \
    --directory=/data \
    --ioengine=libaio \
    --iodepth=64 \
    --rw=read \
    --bs=1M \
    --direct=1 \
    --size=10G \
    --numjobs=4 \
    --runtime=60 \
    --group_reporting
```
**参数变化:**
- `--rw=read`: 模式改为顺序读。
- `--bs=1M`: 测试带宽使用较大的块（如1M）。
- `--iodepth` 和 `--numjobs` 可以适当调低，因为顺序读写的并发压力通常不需要那么高。

---

### 3.4 测试最大顺序写 BPS (带宽)

衡量磁盘写入大文件的能力。

```bash
fio --name=seqwrite_bps_test \
    --directory=/data \
    --ioengine=libaio \
    --iodepth=64 \
    --rw=write \
    --bs=1M \
    --direct=1 \
    --size=10G \
    --numjobs=4 \
    --runtime=60 \
    --group_reporting
```
*参数与顺序读类似，仅将 `--rw` 改为 `write`。*

## 4. 如何解读 fio 输出

当 `fio` 命令执行完毕后，会输出一份详细的报告。您需要关注以下几个关键部分：

```
...
Run status group 0 (all jobs):
   READ: bw=224MiB/s (235MB/s), 224MiB/s-224MiB/s (235MB/s-235MB/s), io=13.2GiB (14.2GB), run=60001-60001msec
  WRITE: bw=76.0MiB/s (79.7MB/s), 76.0MiB/s-76.0MiB/s (79.7MB/s-79.7MB/s), io=4560.0MiB (4782MB), run=60001-60001msec

...

  read:
    ...
    iops=57422
    ...
  write:
    ...
    iops=19455
    ...

Disk stats (read/write):
  nvme0n1: ios=1382909/466986, merge=0/0, ticks=...
```

**关键指标定位:**

1.  **IOPS**: 在 `read:` 或 `write:` 块下面，找到 `iops=...` 这一行。这就是该测试模式下的平均IOPS值。
2.  **BPS (带宽)**: 在报告最上方的 `READ:` 或 `WRITE:` 行，找到 `bw=...` (Bandwidth)。这个值就是平均带宽。

## 5. 测试注意事项

- **多次测试取平均值**：磁盘性能可能有波动，建议每种模式运行2-3次，取一个平均值作为最终的基准。
- **清理测试文件**：`fio` 会在测试目录下生成一个大文件。测试完成后，记得手动删除它以释放磁盘空间 (`rm /data/your_test_name.*`)。
- **区分物理盘和文件系统**：在文件系统上测试 (`--directory=/path`) 会受到文件系统本身开销的影响，而直接对裸设备测试 (`--filename=/dev/sdx`) 则更接近物理性能。对于K8s环境，通常在文件系统上测试更符合实际应用场景。
- **容器化环境**：如果您在容器内测试，请确保容器的 `volumeMounts` 指向您想要测试的磁盘。 