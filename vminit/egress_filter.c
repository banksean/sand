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

struct bpf_map_def {
    unsigned int type;
    unsigned int key_size;
    unsigned int value_size;
    unsigned int max_entries;
    unsigned int map_flags;
};

struct bpf_map_def SEC("maps") allowed_ips = {
    .type        = BPF_MAP_TYPE_HASH,
    .key_size    = sizeof(__u32),
    .value_size  = sizeof(__u8),
    .max_entries = 1024,
};

SEC("tc")
int egress_filter(struct __sk_buff *skb) {
    void *data     = (void *)(long)skb->data;
    void *data_end = (void *)(long)skb->data_end;

    struct ethhdr *eth = data;
    if ((void *)(eth + 1) > data_end)
        return TC_ACT_OK;

    // Only filter IPv4
    if (eth->h_proto != bpf_htons(ETH_P_IP))
        return TC_ACT_OK;

    struct iphdr *ip = (void *)(eth + 1);
    if ((void *)(ip + 1) > data_end)
        return TC_ACT_OK;

    // Allow loopback and RFC1918 (intra-sandbox traffic)
    __u32 dst = ip->daddr;
    __u8 b1 = dst & 0xff;
    if (b1 == 127) return TC_ACT_OK;       // loopback
    if (b1 == 10)  return TC_ACT_OK;       // 10.x.x.x
    // 192.168.x.x
    if (b1 == 192 && ((dst >> 8) & 0xff) == 168) return TC_ACT_OK;

    // Allow DNS (port 53) — handled by our proxy, which enforces policy
    if (ip->protocol == IPPROTO_UDP) {
        struct udphdr *udp = (void *)(ip + 1);
        if ((void *)(udp + 1) <= data_end && udp->dest == bpf_htons(53))
            return TC_ACT_OK;
    }

    // Check allowlist map
    __u8 *allowed = bpf_map_lookup_elem(&allowed_ips, &dst);
    if (allowed)
        return TC_ACT_OK;

    // Default deny
    return TC_ACT_SHOT;
}

char __license[] SEC("license") = "GPL";