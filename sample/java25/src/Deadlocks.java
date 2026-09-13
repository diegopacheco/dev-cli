import module java.base;

public class Deadlocks {
    static final Object LEDGER = new Object();
    static final Object INVENTORY = new Object();
    static final ReentrantLock CAPTURE = new ReentrantLock();
    static final ReentrantLock REFUND = new ReentrantLock();

    public static void start() {
        Thread.ofPlatform().name("ledger-writer").daemon(true).start(() -> monitors(LEDGER, INVENTORY));
        Thread.ofPlatform().name("inventory-writer").daemon(true).start(() -> monitors(INVENTORY, LEDGER));
        Thread.ofPlatform().name("payment-capture").daemon(true).start(() -> locks(CAPTURE, REFUND));
        Thread.ofPlatform().name("payment-refund").daemon(true).start(() -> locks(REFUND, CAPTURE));
    }

    static void monitors(Object first, Object second) {
        synchronized (first) {
            Orders.pause(300);
            synchronized (second) {
                Orders.pause(Long.MAX_VALUE);
            }
        }
    }

    static void locks(ReentrantLock first, ReentrantLock second) {
        first.lock();
        try {
            Orders.pause(300);
            second.lock();
            second.unlock();
        } finally {
            first.unlock();
        }
    }
}
