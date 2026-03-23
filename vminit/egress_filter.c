// egress_filter.c
#include <linux/bpf.h>
#include <linux/pkt_cls.h>
#include <linux/ip.h>
#include <linux/tcp.h>
#include <linux/udp.h>
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_endian.h>
#include <linux/if_ether.h>

#define IPPROTO_UDP 17

struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 1024);
    __type(key, __u32);
    __type(value, __u8);
} allowed_ips SEC(".maps");

SEC("tc")
int egress_filter(struct __sk_buff *skb) {
    void *data     = (void *)(long)skb->data;
    void *data_end = (void *)(long)skb->data_end;

    struct ethhdr *eth = data;
    if ((void *)(eth + 1) > data_end)
        return TC_ACT_OK;

    // Only filter IPv4 (TODO: IPv6)
    if (eth->h_proto != bpf_htons(ETH_P_IP))
        return TC_ACT_OK;

    struct iphdr *ip = (void *)(eth + 1);
    if ((void *)(ip + 1) > data_end)
        return TC_ACT_OK;

    // Allow loopback and RFC1918 (intra-sandbox traffic)
    __u32 dst = ip->daddr;
    __u8 b1 = dst & 0xff;
    __u8 b2 = (dst >> 8) & 0xff;
    if (b1 == 127) return TC_ACT_OK;       // loopback
    if (b1 == 10)  return TC_ACT_OK;       // 10.x.x.x
    if (b1 == 172 && b2 >= 16 && b2 <= 31) return TC_ACT_OK;  // 172.16.0.0/12
    if (b1 == 192 && b2 == 168) return TC_ACT_OK;             // 192.168.x.x

    // Allow DNS only to local proxy (127.0.0.1:53). If we *don't* implement this
    // restriction, we leave open a path to exfiltration via "DNS tunneling": stuffing
    // exfil data into query/name fields and sending it to the IP of a controlled
    // nameserver as though it were regular DNS traffic.
    if (ip->protocol == IPPROTO_UDP) {
        struct udphdr *udp = (void *)(ip + 1);
        __u32 loopback = bpf_htonl(0x7f000001);
        if ((void *)(udp + 1) <= data_end &&
            udp->dest == bpf_htons(53) &&
            dst == loopback)
            return TC_ACT_OK;
    }

    // Check allowlist map
    __u8 *allowed = bpf_map_lookup_elem(&allowed_ips, &dst);
    if (allowed)
        return TC_ACT_OK;

    // TODO: return explicit ICMP port-unreachable/host-unreachable instead of letting
    // denied requests time out. IIUC, doing so is tricky to implement here without
    // either iptables (requires a large redesign of this whole setup), or
    // doing a lot of work to craft and inject a new packet (cloning/rewriting MAC
    // and IP headers, ICMP stuff and probably fixing checksums, etc).

    // Default deny
    return TC_ACT_SHOT;
}

char __license[] SEC("license") = "GPL";