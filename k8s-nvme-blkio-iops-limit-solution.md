# Kubernetes NVMe 磁盘 IOPS 限速方案

## 1. 集群环境信息

- **Kubernetes 版本**：1.20
- **容器运行时**：Docker
- **cgroup 版本**：v1
- **内核版本**: 5.15.0-84, 建议使用 Linux 内核 4.9 及以上版本
- **宿主机磁盘类型**：NVMe（如 `/dev/nvme1n1p1` 挂载到 `/data`）
- **目标**：所有容器共享 `/data`，限制每个容器 IOPS，防止单容器高 IO 影响宿主机

---

## 2. 方案原理

- 利用 Linux cgroup v1 的 blkio throttle 机制对物理 NVMe 盘进行 IOPS 限速。
- **注意**：对 NVMe 设备，blkio throttle 只对整个盘（如 `/dev/nvme1n1`），不对分区（如 `/dev/nvme1n1p1`）生效。
- 通过 DaemonSet 自动化脚本，定期为所有容器设置 blkio 限速。

`blkio` 常用参数有：
```text
blkio.throttle.read_bps_device：限制每秒读取字节数
blkio.throttle.write_bps_device：限制每秒写入字节数
blkio.throttle.read_iops_device：限制每秒读取次数
blkio.throttle.write_iops_device：限制每秒写入次数
```


## 3. 方案测试

### a 限速前
```bash
root@mydemo-my-demo-hl-zk5dv:/data/services/mydemo# fio --name=test-iops --directory=/data --rw=randrw --bs=4k --size=100M --numjobs=1 --iodepth=1 --runtime=60 --time_based --group_reporting
test-iops: (g=0): rw=randrw, bs=(R) 4096B-4096B, (W) 4096B-4096B, (T) 4096B-4096B, ioengine=psync, iodepth=1
fio-3.28
Starting 1 process
test-iops: Laying out IO file (1 file / 100MiB)
Jobs: 1 (f=1): [m(1)][100.0%][r=6872KiB/s,w=6880KiB/s][r=1718,w=1720 IOPS][eta 00m:00s]
test-iops: (groupid=0, jobs=1): err= 0: pid=65406: Wed Jun 18 11:57:02 2025
  read: IOPS=1528, BW=6113KiB/s (6260kB/s)(358MiB/60002msec)
    clat (nsec): min=711, max=13583k, avg=644293.52, stdev=493611.00
     lat (nsec): min=738, max=13583k, avg=644345.43, stdev=493615.78
    clat percentiles (usec):
     |  1.00th=[  396],  5.00th=[  486], 10.00th=[  498], 20.00th=[  515],
     | 30.00th=[  523], 40.00th=[  537], 50.00th=[  553], 60.00th=[  578],
     | 70.00th=[  594], 80.00th=[  611], 90.00th=[  701], 95.00th=[  938],
     | 99.00th=[ 3785], 99.50th=[ 4228], 99.90th=[ 5932], 99.95th=[ 6259],
     | 99.99th=[ 9372]
   bw (  KiB/s): min= 1184, max= 7264, per=99.91%, avg=6107.90, stdev=1592.47, samples=119
   iops        : min=  296, max= 1816, avg=1526.97, stdev=398.12, samples=119
  write: IOPS=1523, BW=6094KiB/s (6241kB/s)(357MiB/60002msec); 0 zone resets
    clat (nsec): min=1234, max=3140.9k, avg=3560.92, stdev=19400.21
     lat (nsec): min=1271, max=3141.0k, avg=3631.72, stdev=19406.61
    clat percentiles (nsec):
     |  1.00th=[  1576],  5.00th=[  1736], 10.00th=[  1816], 20.00th=[  1944],
     | 30.00th=[  2064], 40.00th=[  2224], 50.00th=[  2416], 60.00th=[  2672],
     | 70.00th=[  2960], 80.00th=[  3504], 90.00th=[  5088], 95.00th=[  7264],
     | 99.00th=[ 12864], 99.50th=[ 23936], 99.90th=[118272], 99.95th=[144384],
     | 99.99th=[236544]
   bw (  KiB/s): min= 1184, max= 7656, per=99.90%, avg=6088.94, stdev=1605.40, samples=119
   iops        : min=  296, max= 1914, avg=1522.24, stdev=401.35, samples=119
  lat (nsec)   : 750=0.01%, 1000=0.01%
  lat (usec)   : 2=12.23%, 4=30.29%, 10=6.57%, 20=0.70%, 50=0.11%
  lat (usec)   : 100=0.11%, 250=0.11%, 500=5.40%, 750=40.23%, 1000=2.06%
  lat (msec)   : 2=1.22%, 4=0.62%, 10=0.34%, 20=0.01%
  cpu          : usr=0.32%, sys=1.73%, ctx=94459, majf=3, minf=16
  IO depths    : 1=100.0%, 2=0.0%, 4=0.0%, 8=0.0%, 16=0.0%, 32=0.0%, >=64=0.0%
     submit    : 0=0.0%, 4=100.0%, 8=0.0%, 16=0.0%, 32=0.0%, 64=0.0%, >=64=0.0%
     complete  : 0=0.0%, 4=100.0%, 8=0.0%, 16=0.0%, 32=0.0%, 64=0.0%, >=64=0.0%
     issued rwts: total=91695,91419,0,0 short=0,0,0,0 dropped=0,0,0,0
     latency   : target=0, window=0, percentile=100.00%, depth=1

Run status group 0 (all jobs):
   READ: bw=6113KiB/s (6260kB/s), 6113KiB/s-6113KiB/s (6260kB/s-6260kB/s), io=358MiB (376MB), run=60002-60002msec
  WRITE: bw=6094KiB/s (6241kB/s), 6094KiB/s-6094KiB/s (6241kB/s-6241kB/s), io=357MiB (374MB), run=60002-60002msec
```

```bash
cat /sys/fs/cgroup/blkio/kubepods/burstable/pod3b6b7331-aea4-431c-a683-237fb0f1091e/63e6f6f463df4fb2fbaccd0ecad58e5a6b0d94fc3c16dac5cc1f3b031dd4ca54/blkio.throttle.read_iops_device
259:0 500
```

### b 限速后

```bash
root@localhost:/sys/fs/cgroup/blkio/kubepods/burstable/pod3b6b7331-aea4-431c-a683-237fb0f1091e/63e6f6f463df4fb2fbaccd0ecad58e5a6b0d94fc3c16dac5cc1f3b031dd4ca54# cat blkio.throttle.read_iops_device blkio.throttle.write_iops_device
259:0 500
259:0 500
root@localhost:/sys/fs/cgroup/blkio/kubepods/burstable/pod3b6b7331-aea4-431c-a683-237fb0f1091e/63e6f6f463df4fb2fbaccd0ecad58e5a6b0d94fc3c16dac5cc1f3b031dd4ca54# docker  exec -ti 63e6f6f fio --name=test-iops --directory=/data --rw=randrw --bs=4k --size=100M --numjobs=1 --iodepth=1 --runtime=60 --time_based --group_reporting --direct=1
test-iops: (g=0): rw=randrw, bs=(R) 4096B-4096B, (W) 4096B-4096B, (T) 4096B-4096B, ioengine=psync, iodepth=1
fio-3.28
Starting 1 process
Jobs: 1 (f=1): [m(1)][100.0%][r=1612KiB/s,w=1940KiB/s][r=403,w=485 IOPS][eta 00m:00s]
test-iops: (groupid=0, jobs=1): err= 0: pid=65736: Wed Jun 18 14:12:04 2025
  read: IOPS=466, BW=1866KiB/s (1911kB/s)(109MiB/60006msec)
    clat (usec): min=277, max=65980, avg=949.17, stdev=3577.82
     lat (usec): min=277, max=65981, avg=949.25, stdev=3577.82
    clat percentiles (usec):
     |  1.00th=[  416],  5.00th=[  490], 10.00th=[  498], 20.00th=[  515],
     | 30.00th=[  529], 40.00th=[  537], 50.00th=[  562], 60.00th=[  578],
     | 70.00th=[  586], 80.00th=[  603], 90.00th=[  660], 95.00th=[  840],
     | 99.00th=[23725], 99.50th=[32375], 99.90th=[47973], 99.95th=[55837],
     | 99.99th=[61080]
   bw (  KiB/s): min= 1584, max= 2056, per=100.00%, avg=1867.26, stdev=105.59, samples=119
   iops        : min=  396, max=  514, avg=466.82, stdev=26.40, samples=119
  write: IOPS=466, BW=1867KiB/s (1912kB/s)(109MiB/60006msec); 0 zone resets
    clat (usec): min=227, max=74968, avg=1191.97, stdev=3757.20
     lat (usec): min=227, max=74968, avg=1192.09, stdev=3757.20
    clat percentiles (usec):
     |  1.00th=[  265],  5.00th=[  322], 10.00th=[  429], 20.00th=[  816],
     | 30.00th=[  840], 40.00th=[  857], 50.00th=[  873], 60.00th=[  881],
     | 70.00th=[  889], 80.00th=[  906], 90.00th=[  922], 95.00th=[  955],
     | 99.00th=[21365], 99.50th=[34341], 99.90th=[56886], 99.95th=[61080],
     | 99.99th=[67634]
   bw (  KiB/s): min= 1552, max= 2040, per=100.00%, avg=1868.67, stdev=100.70, samples=119
   iops        : min=  388, max=  510, avg=467.17, stdev=25.18, samples=119
  lat (usec)   : 250=0.12%, 500=10.76%, 750=42.05%, 1000=44.17%
  lat (msec)   : 2=1.42%, 4=0.26%, 10=0.16%, 20=0.02%, 50=0.93%
  lat (msec)   : 100=0.11%
  cpu          : usr=0.19%, sys=0.59%, ctx=56111, majf=0, minf=14
  IO depths    : 1=100.0%, 2=0.0%, 4=0.0%, 8=0.0%, 16=0.0%, 32=0.0%, >=64=0.0%
     submit    : 0=0.0%, 4=100.0%, 8=0.0%, 16=0.0%, 32=0.0%, 64=0.0%, >=64=0.0%
     complete  : 0=0.0%, 4=100.0%, 8=0.0%, 16=0.0%, 32=0.0%, 64=0.0%, >=64=0.0%
     issued rwts: total=27989,28006,0,0 short=0,0,0,0 dropped=0,0,0,0
     latency   : target=0, window=0, percentile=100.00%, depth=1

Run status group 0 (all jobs):
   READ: bw=1866KiB/s (1911kB/s), 1866KiB/s-1866KiB/s (1911kB/s-1911kB/s), io=109MiB (115MB), run=60006-60006msec
  WRITE: bw=1867KiB/s (1912kB/s), 1867KiB/s-1867KiB/s (1912kB/s-1912kB/s), io=109MiB (115MB), run=60006-60006msec
root@localhost:/sys/fs/cgroup/blkio/kubepods/burstable/pod3b6b7331-aea4-431c-a683-237fb0f1091e/63e6f6f463df4fb2fbaccd0ecad58e5a6b0d94fc3c16dac5cc1f3b031dd4ca54# cat blkio.throttle.read_iops_device blkio.throttle.write_iops_device
259:0 1000
259:0 500
root@localhost:/sys/fs/cgroup/blkio/kubepods/burstable/pod3b6b7331-aea4-431c-a683-237fb0f1091e/63e6f6f463df4fb2fbaccd0ecad58e5a6b0d94fc3c16dac5cc1f3b031dd4ca54# docker  exec -ti 63e6f6f fio --name=test-iops --directory=/data --rw=randrw --bs=4k --size=100M --numjobs=1 --iodepth=1 --runtime=60 --time_based --group_reporting --direct=1
test-iops: (g=0): rw=randrw, bs=(R) 4096B-4096B, (W) 4096B-4096B, (T) 4096B-4096B, ioengine=psync, iodepth=1
fio-3.28
Starting 1 process
Jobs: 1 (f=1): [m(1)][100.0%][r=2192KiB/s,w=2000KiB/s][r=548,w=500 IOPS][eta 00m:00s]
test-iops: (groupid=0, jobs=1): err= 0: pid=65755: Wed Jun 18 14:14:51 2025
  read: IOPS=498, BW=1992KiB/s (2040kB/s)(117MiB/60004msec)
    clat (usec): min=140, max=10189, avg=589.65, stdev=248.14
     lat (usec): min=140, max=10189, avg=589.73, stdev=248.14
    clat percentiles (usec):
     |  1.00th=[  441],  5.00th=[  490], 10.00th=[  498], 20.00th=[  515],
     | 30.00th=[  523], 40.00th=[  537], 50.00th=[  553], 60.00th=[  570],
     | 70.00th=[  586], 80.00th=[  603], 90.00th=[  668], 95.00th=[  807],
     | 99.00th=[ 1221], 99.50th=[ 1598], 99.90th=[ 4178], 99.95th=[ 5538],
     | 99.99th=[ 6980]
   bw (  KiB/s): min= 1544, max= 2536, per=99.85%, avg=1989.48, stdev=193.73, samples=119
   iops        : min=  386, max=  634, avg=497.36, stdev=48.45, samples=119
  write: IOPS=497, BW=1992KiB/s (2040kB/s)(117MiB/60004msec); 0 zone resets
    clat (usec): min=227, max=71132, avg=1416.43, stdev=4024.14
     lat (usec): min=227, max=71132, avg=1416.54, stdev=4024.14
    clat percentiles (usec):
     |  1.00th=[  285],  5.00th=[  791], 10.00th=[  816], 20.00th=[  832],
     | 30.00th=[  848], 40.00th=[  865], 50.00th=[  881], 60.00th=[  889],
     | 70.00th=[  898], 80.00th=[  906], 90.00th=[  930], 95.00th=[  963],
     | 99.00th=[27919], 99.50th=[32637], 99.90th=[41157], 99.95th=[52691],
     | 99.99th=[61604]
   bw (  KiB/s): min= 1662, max= 2120, per=100.00%, avg=1992.55, stdev=53.43, samples=119
   iops        : min=  415, max=  530, avg=498.13, stdev=13.38, samples=119
  lat (usec)   : 250=0.05%, 500=6.70%, 750=42.10%, 1000=48.36%
  lat (msec)   : 2=1.34%, 4=0.31%, 10=0.16%, 20=0.12%, 50=0.83%
  lat (msec)   : 100=0.03%
  cpu          : usr=0.23%, sys=0.60%, ctx=59865, majf=0, minf=13
  IO depths    : 1=100.0%, 2=0.0%, 4=0.0%, 8=0.0%, 16=0.0%, 32=0.0%, >=64=0.0%
     submit    : 0=0.0%, 4=100.0%, 8=0.0%, 16=0.0%, 32=0.0%, 64=0.0%, >=64=0.0%
     complete  : 0=0.0%, 4=100.0%, 8=0.0%, 16=0.0%, 32=0.0%, 64=0.0%, >=64=0.0%
     issued rwts: total=29882,29880,0,0 short=0,0,0,0 dropped=0,0,0,0
     latency   : target=0, window=0, percentile=100.00%, depth=1

Run status group 0 (all jobs):
   READ: bw=1992KiB/s (2040kB/s), 1992KiB/s-1992KiB/s (2040kB/s-2040kB/s), io=117MiB (122MB), run=60004-60004msec
  WRITE: bw=1992KiB/s (2040kB/s), 1992KiB/s-1992KiB/s (2040kB/s-2040kB/s), io=117MiB (122MB), run=60004-60004msec
root@localhost:/sys/fs/cgroup/blkio/kubepods/burstable/pod3b6b7331-aea4-431c-a683-237fb0f1091e/63e6f6f463df4fb2fbaccd0ecad58e5a6b0d94fc3c16dac5cc1f3b031dd4ca54# cat blkio.throttle.read_iops_device blkio.throttle.write_iops_device
259:0 1000
259:0 500
root@localhost:/sys/fs/cgroup/blkio/kubepods/burstable/pod3b6b7331-aea4-431c-a683-237fb0f1091e/63e6f6f463df4fb2fbaccd0ecad58e5a6b0d94fc3c16dac5cc1f3b031dd4ca54# docker  exec -ti 63e6f6f fio --name=test-iops --directory=/data --rw=randrw --bs=4k --size=100M --numjobs=1 --iodepth=1 --runtime=60 --time_based --group_reporting --direct=0
test-iops: (g=0): rw=randrw, bs=(R) 4096B-4096B, (W) 4096B-4096B, (T) 4096B-4096B, ioengine=psync, iodepth=1
fio-3.28
Starting 1 process
Jobs: 1 (f=1): [m(1)][100.0%][r=2404KiB/s,w=2308KiB/s][r=601,w=577 IOPS][eta 00m:00s]
test-iops: (groupid=0, jobs=1): err= 0: pid=65768: Wed Jun 18 14:16:33 2025
  read: IOPS=1033, BW=4136KiB/s (4235kB/s)(242MiB/60005msec)
    clat (nsec): min=856, max=62214k, avg=960894.73, stdev=3925464.46
     lat (nsec): min=882, max=62214k, avg=960948.49, stdev=3925464.00
    clat percentiles (nsec):
     |  1.00th=[    1224],  5.00th=[    1800], 10.00th=[  448512],
     | 20.00th=[  501760], 30.00th=[  514048], 40.00th=[  528384],
     | 50.00th=[  544768], 60.00th=[  561152], 70.00th=[  585728],
     | 80.00th=[  618496], 90.00th=[  724992], 95.00th=[  962560],
     | 99.00th=[ 6127616], 99.50th=[41680896], 99.90th=[46399488],
     | 99.95th=[47448064], 99.99th=[50069504]
   bw (  KiB/s): min= 1552, max= 4672, per=100.00%, avg=4157.97, stdev=546.62, samples=119
   iops        : min=  388, max= 1168, avg=1039.49, stdev=136.66, samples=119
  write: IOPS=1029, BW=4119KiB/s (4218kB/s)(241MiB/60005msec); 0 zone resets
    clat (nsec): min=1216, max=1632.3k, avg=3783.30, stdev=14541.05
     lat (nsec): min=1256, max=1632.4k, avg=3855.15, stdev=14549.43
    clat percentiles (nsec):
     |  1.00th=[  1560],  5.00th=[  1720], 10.00th=[  1816], 20.00th=[  1944],
     | 30.00th=[  2064], 40.00th=[  2224], 50.00th=[  2416], 60.00th=[  2640],
     | 70.00th=[  2992], 80.00th=[  3568], 90.00th=[  5344], 95.00th=[  7584],
     | 99.00th=[ 13632], 99.50th=[ 67072], 99.90th=[150528], 99.95th=[179200],
     | 99.99th=[268288]
   bw (  KiB/s): min= 1488, max= 4984, per=100.00%, avg=4142.17, stdev=604.14, samples=119
   iops        : min=  372, max= 1246, avg=1035.54, stdev=151.03, samples=119
  lat (nsec)   : 1000=0.03%
  lat (usec)   : 2=14.98%, 4=30.26%, 10=6.94%, 20=0.86%, 50=0.07%
  lat (usec)   : 100=0.12%, 250=0.15%, 500=6.40%, 750=35.66%, 1000=2.18%
  lat (msec)   : 2=1.17%, 4=0.47%, 10=0.26%, 20=0.01%, 50=0.44%
  lat (msec)   : 100=0.01%
  cpu          : usr=0.19%, sys=1.25%, ctx=58049, majf=0, minf=18
  IO depths    : 1=100.0%, 2=0.0%, 4=0.0%, 8=0.0%, 16=0.0%, 32=0.0%, >=64=0.0%
     submit    : 0=0.0%, 4=100.0%, 8=0.0%, 16=0.0%, 32=0.0%, 64=0.0%, >=64=0.0%
     complete  : 0=0.0%, 4=100.0%, 8=0.0%, 16=0.0%, 32=0.0%, 64=0.0%, >=64=0.0%
     issued rwts: total=62041,61789,0,0 short=0,0,0,0 dropped=0,0,0,0
     latency   : target=0, window=0, percentile=100.00%, depth=1

Run status group 0 (all jobs):
   READ: bw=4136KiB/s (4235kB/s), 4136KiB/s-4136KiB/s (4235kB/s-4235kB/s), io=242MiB (254MB), run=60005-60005msec
  WRITE: bw=4119KiB/s (4218kB/s), 4119KiB/s-4119KiB/s (4218kB/s-4218kB/s), io=241MiB (253MB), run=60005-60005msec

root@localhost:/sys/fs/cgroup/blkio/kubepods/burstable/pod3b6b7331-aea4-431c-a683-237fb0f1091e/63e6f6f463df4fb2fbaccd0ecad58e5a6b0d94fc3c16dac5cc1f3b031dd4ca54# cat blkio.throttle.read_bps_device  blkio.throttle.write_bps_device
259:0 1048576
259:0 1048576
root@localhost:/sys/fs/cgroup/blkio/kubepods/burstable/pod3b6b7331-aea4-431c-a683-237fb0f1091e/63e6f6f463df4fb2fbaccd0ecad58e5a6b0d94fc3c16dac5cc1f3b031dd4ca54# docker  exec -ti 63e6f6f fio --name=test-iops --directory=/data --rw=randrw --bs=4k --size=100M --numjobs=1 --iodepth=1 --runtime=60 --time_based --group_reporting --direct=1
test-iops: (g=0): rw=randrw, bs=(R) 4096B-4096B, (W) 4096B-4096B, (T) 4096B-4096B, ioengine=psync, iodepth=1
fio-3.28
Starting 1 process
Jobs: 1 (f=1): [m(1)][100.0%][r=928KiB/s,w=932KiB/s][r=232,w=233 IOPS][eta 00m:00s]
test-iops: (groupid=0, jobs=1): err= 0: pid=65787: Wed Jun 18 14:37:50 2025
  read: IOPS=232, BW=931KiB/s (953kB/s)(54.6MiB/60021msec)
    clat (usec): min=282, max=85118, avg=2112.22, stdev=10270.98
     lat (usec): min=282, max=85118, avg=2112.30, stdev=10270.97
    clat percentiles (usec):
     |  1.00th=[  375],  5.00th=[  478], 10.00th=[  494], 20.00th=[  510],
     | 30.00th=[  519], 40.00th=[  529], 50.00th=[  545], 60.00th=[  562],
     | 70.00th=[  586], 80.00th=[  611], 90.00th=[  709], 95.00th=[  930],
     | 99.00th=[70779], 99.50th=[77071], 99.90th=[82314], 99.95th=[83362],
     | 99.99th=[83362]
   bw (  KiB/s): min=  728, max= 1024, per=100.00%, avg=931.12, stdev=69.59, samples=120
   iops        : min=  182, max=  256, avg=232.78, stdev=17.39, samples=120
  write: IOPS=233, BW=932KiB/s (954kB/s)(54.6MiB/60021msec); 0 zone resets
    clat (usec): min=239, max=90173, avg=2179.51, stdev=9967.95
     lat (usec): min=239, max=90173, avg=2179.62, stdev=9967.95
    clat percentiles (usec):
     |  1.00th=[  262],  5.00th=[  289], 10.00th=[  314], 20.00th=[  375],
     | 30.00th=[  685], 40.00th=[  824], 50.00th=[  848], 60.00th=[  865],
     | 70.00th=[  881], 80.00th=[  898], 90.00th=[  930], 95.00th=[  963],
     | 99.00th=[68682], 99.50th=[76022], 99.90th=[83362], 99.95th=[84411],
     | 99.99th=[86508]
   bw (  KiB/s): min=  680, max= 1024, per=100.00%, avg=932.25, stdev=69.24, samples=120
   iops        : min=  170, max=  256, avg=233.06, stdev=17.31, samples=120
  lat (usec)   : 250=0.10%, 500=20.44%, 750=40.28%, 1000=35.30%
  lat (msec)   : 2=1.41%, 4=0.19%, 10=0.14%, 20=0.01%, 50=0.01%
  lat (msec)   : 100=2.13%
  cpu          : usr=0.09%, sys=0.32%, ctx=28010, majf=0, minf=14
  IO depths    : 1=100.0%, 2=0.0%, 4=0.0%, 8=0.0%, 16=0.0%, 32=0.0%, >=64=0.0%
     submit    : 0=0.0%, 4=100.0%, 8=0.0%, 16=0.0%, 32=0.0%, 64=0.0%, >=64=0.0%
     complete  : 0=0.0%, 4=100.0%, 8=0.0%, 16=0.0%, 32=0.0%, 64=0.0%, >=64=0.0%
     issued rwts: total=13967,13985,0,0 short=0,0,0,0 dropped=0,0,0,0
     latency   : target=0, window=0, percentile=100.00%, depth=1

Run status group 0 (all jobs):
   READ: bw=931KiB/s (953kB/s), 931KiB/s-931KiB/s (953kB/s-953kB/s), io=54.6MiB (57.2MB), run=60021-60021msec
  WRITE: bw=932KiB/s (954kB/s), 932KiB/s-932KiB/s (954kB/s-954kB/s), io=54.6MiB (57.3MB), run=60021-60021msec
```

可以看到限速前读写的 `iops` 在 `1500` 左右,这是因为申请的磁盘 `iops` 读写总共 `3000`;
限速后读写的 `iops` 在 `500` 左右,符合写入的限速要求；

--direct=1：IO 绕过页缓存，直接作用于块设备

#### 场景一: --direct=1

限制: 读 1000 iops,写 500 iops, 此时取最小 `iops` 限制

#### 场景二: --direct=0

限制: 读 1000 iops,写 500 iops, 此时取最大 `iops` 限制

#### 场景三: 限制磁盘吞吐 1MB

1048576 = 1 * 1024 * 1024（1MB）

---

## 4. 自动化限速脚本

```bash
#!/bin/bash

CONTAINER_IOPS_LIMIT=${CONTAINER_IOPS_LIMIT:-500}
DATA_TOTAL_IOPS=${DATA_TOTAL_IOPS:-3000}
DATA_MOUNT=${DATA_MOUNT:-/data}
EXCLUDE_KEYWORDS=${EXCLUDE_KEYWORDS:-pause}

# 处理关键字为数组
IFS=',' read -ra EXCLUDE_ARR <<< "$EXCLUDE_KEYWORDS"

DATA_DEV=$(df "$DATA_MOUNT" | tail -1 | awk '{print $1}')
PARENT_DEV=$(lsblk -no PKNAME "$DATA_DEV")
MAJ_MIN=$(lsblk -no MAJ:MIN "/dev/$PARENT_DEV" | head -1)

if [ -z "$MAJ_MIN" ]; then
  echo "未能识别/data挂载盘的父设备号，跳过IOPS限制"
  exit 0
fi

for cid in $(docker ps -q); do
  IMAGE=$(docker inspect --format '{{.Config.Image}}' "$cid")
  NAME=$(docker inspect --format '{{.Name}}' "$cid")

  # 多关键字过滤
  SKIP=0
  for kw in "${EXCLUDE_ARR[@]}"; do
    if [[ "$IMAGE" == *"$kw"* ]] || [[ "$NAME" == *"$kw"* ]]; then
      SKIP=1
      break
    fi
  done
  [ "$SKIP" -eq 1 ] && continue

  CGROUP_PARENT=$(docker inspect --format '{{.HostConfig.CgroupParent}}' "$cid")
  ID=$(docker inspect --format '{{.Id}}' "$cid")

  if [ -z "$CGROUP_PARENT" ] || [ "$CGROUP_PARENT" == "/" ]; then
    CGROUP_PATH="/sys/fs/cgroup/blkio/docker/$ID"
  else
    CGROUP_PARENT_CLEAN=${CGROUP_PARENT#/}
    CGROUP_PATH="/sys/fs/cgroup/blkio/$CGROUP_PARENT_CLEAN/$ID"
  fi

  if [ -d "$CGROUP_PATH" ]; then
    echo "$MAJ_MIN $CONTAINER_IOPS_LIMIT" > "$CGROUP_PATH/blkio.throttle.read_iops_device"
    echo "$MAJ_MIN $CONTAINER_IOPS_LIMIT" > "$CGROUP_PATH/blkio.throttle.write_iops_device"
  fi
done

HOST_CGROUP_PATH="/sys/fs/cgroup/blkio"
if [ -d "$HOST_CGROUP_PATH" ]; then
  echo "$MAJ_MIN $DATA_TOTAL_IOPS" > "$HOST_CGROUP_PATH/blkio.throttle.read_iops_device"
  echo "$MAJ_MIN $DATA_TOTAL_IOPS" > "$HOST_CGROUP_PATH/blkio.throttle.write_iops_device"
fi
```

---

## 5. Dockerfile

```dockerfile
FROM docker:24.0.7-cli

RUN apk add --no-cache bash coreutils util-linux

COPY blkio-iops.sh /script/blkio-iops.sh
RUN chmod +x /script/blkio-iops.sh

CMD ["bash", "-c", "while true; do /script/blkio-iops.sh; sleep 30; done"]
```

---

## 6. DaemonSet YAML

```yaml
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: blkio-iops-throttle
  namespace: kube-system
spec:
  selector:
    matchLabels:
      name: blkio-iops-throttle
  template:
    metadata:
      labels:
        name: blkio-iops-throttle
    spec:
      hostPID: true
      containers:
      - name: blkio-iops-throttle
        image: your-repo/blkio-iops-throttle:latest
        securityContext:
          privileged: true
        env:
        - name: CONTAINER_IOPS_LIMIT
          value: "500"
        - name: DATA_TOTAL_IOPS
          value: "3000"
        - name: DATA_MOUNT
          value: "/data"
        - name: EXCLUDE_KEYWORDS
          value: "pause,istio-proxy"
        volumeMounts:
        - name: cgroup
          mountPath: /sys/fs/cgroup
        - name: docker-socket
          mountPath: /var/run/docker.sock
        - name: script
          mountPath: /script
      volumes:
      - name: cgroup
        hostPath:
          path: /sys/fs/cgroup
      - name: docker-socket
        hostPath:
          path: /var/run/docker.sock
      - name: script
        configMap:
          name: blkio-iops-script
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: blkio-iops-script
  namespace: kube-system
data:
  blkio-iops.sh: |
    #!/bin/bash
    CONTAINER_IOPS_LIMIT=${CONTAINER_IOPS_LIMIT:-500}
    DATA_TOTAL_IOPS=${DATA_TOTAL_IOPS:-3000}
    DATA_MOUNT=${DATA_MOUNT:-/data}
    EXCLUDE_KEYWORDS=${EXCLUDE_KEYWORDS:-pause}
    IFS=',' read -ra EXCLUDE_ARR <<< "$EXCLUDE_KEYWORDS"
    DATA_DEV=$(df "$DATA_MOUNT" | tail -1 | awk '{print $1}')
    PARENT_DEV=$(lsblk -no PKNAME "$DATA_DEV")
    MAJ_MIN=$(lsblk -no MAJ:MIN "/dev/$PARENT_DEV" | head -1)
    if [ -z "$MAJ_MIN" ]; then
      echo "未能识别/data挂载盘的父设备号，跳过IOPS限制"
      exit 0
    fi
    for cid in $(docker ps -q); do
      IMAGE=$(docker inspect --format '{{.Config.Image}}' "$cid")
      NAME=$(docker inspect --format '{{.Name}}' "$cid")
      SKIP=0
      for kw in "${EXCLUDE_ARR[@]}"; do
        if [[ "$IMAGE" == *"$kw"* ]] || [[ "$NAME" == *"$kw"* ]]; then
          SKIP=1
          break
        fi
      done
      [ "$SKIP" -eq 1 ] && continue
      CGROUP_PARENT=$(docker inspect --format '{{.HostConfig.CgroupParent}}' "$cid")
      ID=$(docker inspect --format '{{.Id}}' "$cid")
      if [ -z "$CGROUP_PARENT" ] || [ "$CGROUP_PARENT" == "/" ]; then
        CGROUP_PATH="/sys/fs/cgroup/blkio/docker/$ID"
      else
        CGROUP_PARENT_CLEAN=${CGROUP_PARENT#/}
        CGROUP_PATH="/sys/fs/cgroup/blkio/$CGROUP_PARENT_CLEAN/$ID"
      fi
      if [ -d "$CGROUP_PATH" ]; then
        echo "$MAJ_MIN $CONTAINER_IOPS_LIMIT" > "$CGROUP_PATH/blkio.throttle.read_iops_device"
        echo "$MAJ_MIN $CONTAINER_IOPS_LIMIT" > "$CGROUP_PATH/blkio.throttle.write_iops_device"
      fi
    done
    HOST_CGROUP_PATH="/sys/fs/cgroup/blkio"
    if [ -d "$HOST_CGROUP_PATH" ]; then
      echo "$MAJ_MIN $DATA_TOTAL_IOPS" > "$HOST_CGROUP_PATH/blkio.throttle.read_iops_device"
      echo "$MAJ_MIN $DATA_TOTAL_IOPS" > "$HOST_CGROUP_PATH/blkio.throttle.write_iops_device"
    fi
```

---

## 7. 基于容器创建事件的自动化磁盘IO隔离方案

### 方案目标
- 实时监听本机容器创建事件（支持 Docker 或 containerd 运行时）。
- 过滤掉无需限速的容器（如 pause、istio-proxy 等）。
- 自动对新创建的业务容器执行磁盘 IOPS 限速操作。

### 方案流程
1. **事件监听**：
   - Docker 环境：使用 `docker events --filter type=container --filter event=create` 实时监听容器创建。
   - containerd 环境：使用 `ctr events | grep container-create` 或 `ctr --namespace k8s.io events | grep container-create` 实时监听。
2. **事件处理**：
   - 捕获到新容器创建事件后，获取容器ID。
   - 通过 `docker inspect` 或 `ctr`/`crictl inspect` 获取容器详细信息（如 CgroupParent、Id、镜像名、容器名等）。
   - 过滤掉 pause、istio-proxy 等无需限速的容器。
3. **自动限速**：
   - 拼接 cgroup 路径 `/sys/fs/cgroup/blkio${CgroupParent}/${Id}`。
   - 获取 `/data` 挂载盘的父设备号（如 259:0）。
   - 写入 IOPS 限速参数到对应 cgroup 文件。

### 参考自动化脚本思路（伪代码）

```bash
# 监听容器创建事件（以 Docker 为例）
docker events --filter type=container --filter event=create --format '{{.ID}}' | while read cid; do
  # 获取容器信息
  IMAGE=$(docker inspect --format '{{.Config.Image}}' "$cid")
  NAME=$(docker inspect --format '{{.Name}}' "$cid")
  # 过滤无需限速的容器
  for kw in $(echo "$EXCLUDE_KEYWORDS" | tr ',' ' '); do
    if [[ "$IMAGE" == *"$kw"* ]] || [[ "$NAME" == *"$kw"* ]]; then
      continue 2
    fi
  done
  # 获取cgroup路径
  CGROUP_PARENT=$(docker inspect --format '{{.HostConfig.CgroupParent}}' "$cid")
  ID=$(docker inspect --format '{{.Id}}' "$cid")
  if [ -z "$CGROUP_PARENT" ] || [ "$CGROUP_PARENT" == "/" ]; then
    CGROUP_PATH="/sys/fs/cgroup/blkio/docker/$ID"
  else
    CGROUP_PARENT_CLEAN=${CGROUP_PARENT#/}
    CGROUP_PATH="/sys/fs/cgroup/blkio/$CGROUP_PARENT_CLEAN/$ID"
  fi
  # 获取物理盘设备号
  DATA_DEV=$(df "$DATA_MOUNT" | tail -1 | awk '{print $1}')
  PARENT_DEV=$(lsblk -no PKNAME "$DATA_DEV")
  MAJ_MIN=$(lsblk -no MAJ:MIN "/dev/$PARENT_DEV" | head -1)
  # 写入限速
  if [ -d "$CGROUP_PATH" ]; then
    echo "$MAJ_MIN $CONTAINER_IOPS_LIMIT" > "$CGROUP_PATH/blkio.throttle.read_iops_device"
    echo "$MAJ_MIN $CONTAINER_IOPS_LIMIT" > "$CGROUP_PATH/blkio.throttle.write_iops_device"
  fi
done
```

### 方案优势
- **实时性强**：新容器一创建即限速，无需定时轮询。
- **资源节省**：只处理业务容器，避免对 pause、sidecar 等无关容器限速。
- **易于集成**：可作为独立守护进程或 systemd 服务运行在每台节点。

### 适用场景
- 需要对所有新创建业务容器自动进行磁盘 IO 隔离的 Kubernetes/容器集群。
- 支持 Docker、containerd 运行时。

---

## 8. 支持 Docker/containerd 及 cgroup v1/v2 的完整自动化限速脚本

### 环境变量说明
- `CONTAINER_RUNTIME`：容器运行时，支持 `docker`、`containerd`，默认自动探测。
- `CGROUP_VERSION`：cgroup 版本，支持 `v1`、`v2`，默认自动探测。
- `CONTAINER_IOPS_LIMIT`：单容器 IOPS 限速，默认 500。
- `DATA_TOTAL_IOPS`：数据盘总 IOPS 限速，默认 3000。
- `DATA_MOUNT`：数据盘挂载点，默认 `/data`。
- `EXCLUDE_KEYWORDS`：过滤关键字，逗号分隔，默认 `pause,istio-proxy,psmdb,kube-system,koordinator,apisix`。
- `CONTAINERD_NAMESPACE`：containerd namespace，默认 `k8s.io`。

### cgroup v1/v2 适配说明
- **cgroup v1**：限速文件为 `blkio.throttle.read_iops_device` 和 `blkio.throttle.write_iops_device`，路径如 `/sys/fs/cgroup/blkio/...`。
- **cgroup v2**：限速文件为 `io.max`，路径如 `/sys/fs/cgroup/...`，格式为 `<major:minor> riops=... wiops=...`。

---

```bash
#!/bin/bash

# 环境变量配置
declare -x CONTAINER_RUNTIME=${CONTAINER_RUNTIME:-auto}
declare -x CGROUP_VERSION=${CGROUP_VERSION:-auto}
CONTAINER_IOPS_LIMIT=${CONTAINER_IOPS_LIMIT:-500}
DATA_TOTAL_IOPS=${DATA_TOTAL_IOPS:-3000}
DATA_MOUNT=${DATA_MOUNT:-/data}
EXCLUDE_KEYWORDS=${EXCLUDE_KEYWORDS:-pause,istio-proxy,psmdb,kube-system,koordinator,apisix}
CONTAINERD_NAMESPACE=${CONTAINERD_NAMESPACE:-k8s.io}

# 自动探测运行时
detect_runtime() {
  if [ "$CONTAINER_RUNTIME" = "docker" ] || [ "$CONTAINER_RUNTIME" = "containerd" ]; then
    echo "$CONTAINER_RUNTIME"
  elif command -v docker &>/dev/null; then
    echo "docker"
  elif command -v ctr &>/dev/null; then
    echo "containerd"
  else
    echo "none"
  fi
}
CONTAINER_RUNTIME=$(detect_runtime)
if [ "$CONTAINER_RUNTIME" = "none" ]; then
  echo "No supported container runtime found!" >&2
  exit 1
fi
echo "Using container runtime: $CONTAINER_RUNTIME"

# 自动探测cgroup版本
detect_cgroup_version() {
  if [ "$CGROUP_VERSION" = "v1" ] || [ "$CGROUP_VERSION" = "v2" ]; then
    echo "$CGROUP_VERSION"
  elif [ -f /sys/fs/cgroup/cgroup.controllers ]; then
    echo "v2"
  else
    echo "v1"
  fi
}
CGROUP_VERSION=$(detect_cgroup_version)
echo "Detected cgroup version: $CGROUP_VERSION"

# 过滤函数
should_skip() {
  local image="$1"
  local name="$2"
  IFS=',' read -ra EXCLUDE_ARR <<< "$EXCLUDE_KEYWORDS"
  for kw in "${EXCLUDE_ARR[@]}"; do
    if [[ "$image" == *"$kw"* ]] || [[ "$name" == *"$kw"* ]]; then
      return 0
    fi
  done
  return 1
}

# 获取物理盘设备号
get_maj_min() {
  DATA_DEV=$(df "$DATA_MOUNT" | tail -1 | awk '{print $1}')
  PARENT_DEV=$(lsblk -no PKNAME "$DATA_DEV")
  lsblk -no MAJ:MIN "/dev/$PARENT_DEV" | head -1
}

# cgroup路径查找（containerd v2 需特殊处理）
find_cgroup_path() {
  local cid="$1"
  if [ "$CGROUP_VERSION" = "v1" ]; then
    find /sys/fs/cgroup/blkio/ -type d -name "*$cid*" | head -1
  else
    find /sys/fs/cgroup/ -type d -name "*$cid*" | head -1
  fi
}

# 限速操作
set_limit() {
  local cgroup_path="$1"
  local maj_min="$2"
  if [ -d "$cgroup_path" ] && [ -n "$maj_min" ]; then
    if [ "$CGROUP_VERSION" = "v1" ]; then
      echo "$maj_min $CONTAINER_IOPS_LIMIT" > "$cgroup_path/blkio.throttle.read_iops_device"
      echo "$maj_min $CONTAINER_IOPS_LIMIT" > "$cgroup_path/blkio.throttle.write_iops_device"
      echo "[$(date)] Set IOPS limit at $cgroup_path: $maj_min $CONTAINER_IOPS_LIMIT (v1)"
    else
      echo "$maj_min riops=$CONTAINER_IOPS_LIMIT wiops=$CONTAINER_IOPS_LIMIT" > "$cgroup_path/io.max"
      echo "[$(date)] Set IOPS limit at $cgroup_path: $maj_min riops=$CONTAINER_IOPS_LIMIT wiops=$CONTAINER_IOPS_LIMIT (v2)"
    fi
  else
    echo "[$(date)] Skip: cgroup or device not found ($cgroup_path, $maj_min)"
  fi
}

# 启动时为所有已存在的业务容器限速
if [ "$CONTAINER_RUNTIME" = "docker" ]; then
  for cid in $(docker ps -q); do
    IMAGE=$(docker inspect --format '{{.Config.Image}}' "$cid" 2>/dev/null)
    NAME=$(docker inspect --format '{{.Name}}' "$cid" 2>/dev/null)
    should_skip "$IMAGE" "$NAME" && continue
    CGROUP_PARENT=$(docker inspect --format '{{.HostConfig.CgroupParent}}' "$cid")
    ID=$(docker inspect --format '{{.Id}}' "$cid")
    if [ "$CGROUP_VERSION" = "v1" ]; then
      if [ -z "$CGROUP_PARENT" ] || [ "$CGROUP_PARENT" == "/" ]; then
        CGROUP_PATH="/sys/fs/cgroup/blkio/docker/$ID"
      else
        CGROUP_PARENT_CLEAN=${CGROUP_PARENT#/}
        CGROUP_PATH="/sys/fs/cgroup/blkio/$CGROUP_PARENT_CLEAN/$ID"
      fi
    else
      CGROUP_PATH=$(find_cgroup_path "$ID")
    fi
    MAJ_MIN=$(get_maj_min)
    set_limit "$CGROUP_PATH" "$MAJ_MIN"
  done
elif [ "$CONTAINER_RUNTIME" = "containerd" ]; then
  for cid in $(ctr --namespace "$CONTAINERD_NAMESPACE" containers list -q); do
    IMAGE=$(ctr --namespace "$CONTAINERD_NAMESPACE" containers info "$cid" | grep 'Image:' | awk '{print $2}')
    NAME=$(ctr --namespace "$CONTAINERD_NAMESPACE" containers info "$cid" | grep 'Name:' | awk '{print $2}')
    should_skip "$IMAGE" "$NAME" && continue
    CGROUP_PATH=$(find_cgroup_path "$cid")
    MAJ_MIN=$(get_maj_min)
    set_limit "$CGROUP_PATH" "$MAJ_MIN"
  done
fi

# 监听新容器创建事件
if [ "$CONTAINER_RUNTIME" = "docker" ]; then
  docker events --filter type=container --filter event=create --format '{{.ID}}' | while read cid; do
    IMAGE=$(docker inspect --format '{{.Config.Image}}' "$cid" 2>/dev/null)
    NAME=$(docker inspect --format '{{.Name}}' "$cid" 2>/dev/null)
    should_skip "$IMAGE" "$NAME" && continue
    CGROUP_PARENT=$(docker inspect --format '{{.HostConfig.CgroupParent}}' "$cid")
    ID=$(docker inspect --format '{{.Id}}' "$cid")
    if [ "$CGROUP_VERSION" = "v1" ]; then
      if [ -z "$CGROUP_PARENT" ] || [ "$CGROUP_PARENT" == "/" ]; then
        CGROUP_PATH="/sys/fs/cgroup/blkio/docker/$ID"
      else
        CGROUP_PARENT_CLEAN=${CGROUP_PARENT#/}
        CGROUP_PATH="/sys/fs/cgroup/blkio/$CGROUP_PARENT_CLEAN/$ID"
      fi
    else
      CGROUP_PATH=$(find_cgroup_path "$ID")
    fi
    MAJ_MIN=$(get_maj_min)
    set_limit "$CGROUP_PATH" "$MAJ_MIN"
  done
elif [ "$CONTAINER_RUNTIME" = "containerd" ]; then
  ctr --namespace "$CONTAINERD_NAMESPACE" events | grep container-create | while read line; do
    cid=$(echo "$line" | awk '{print $NF}')
    IMAGE=$(ctr --namespace "$CONTAINERD_NAMESPACE" containers info "$cid" | grep 'Image:' | awk '{print $2}')
    NAME=$(ctr --namespace "$CONTAINERD_NAMESPACE" containers info "$cid" | grep 'Name:' | awk '{print $2}')
    should_skip "$IMAGE" "$NAME" && continue
    CGROUP_PATH=$(find /sys/fs/cgroup/blkio/ -type d -name "*$cid*" | head -1)
    MAJ_MIN=$(get_maj_min)
    set_limit "$CGROUP_PATH" "$MAJ_MIN"
  done
fi
```

---