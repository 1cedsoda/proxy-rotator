//! Fast, good-enough random number generation for non-cryptographic use.

use std::cell::Cell;

/// Fast, good-enough random using a thread-local xorshift64.
///
/// Seeded from the current time and thread ID so different threads and
/// different runs produce different sequences. Not suitable for cryptographic
/// use — only for tie-breaking and session ID generation.
pub fn cheap_random() -> u64 {
    thread_local! {
        static STATE: Cell<u64> = Cell::new({
            let t = std::time::SystemTime::now()
                .duration_since(std::time::UNIX_EPOCH)
                .unwrap_or_default()
                .as_nanos() as u64;
            let tid = std::thread::current().id();
            let tid_bits = format!("{:?}", tid);
            let tid_hash = tid_bits
                .bytes()
                .fold(0u64, |acc, b| acc.wrapping_mul(31).wrapping_add(b as u64));
            t ^ tid_hash ^ 0x517cc1b727220a95
        });
    }
    STATE.with(|s| {
        let mut x = s.get();
        x ^= x << 13;
        x ^= x >> 7;
        x ^= x << 17;
        s.set(x);
        x
    })
}
