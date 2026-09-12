public class DevcliJvm {
    static final Object ledger = new Object();
    static final Object inventory = new Object();

    public static void main(String[] args) throws Exception {
        start("order-worker", () -> lockBoth(ledger, inventory));
        start("stock-worker", () -> lockBoth(inventory, ledger));
        start("scheduler-tick", () -> sleepForever());
        start("queue-consumer", () -> waitForever());
        Thread.currentThread().join();
    }

    static void start(String name, Runnable body) {
        Thread t = new Thread(body, name);
        t.start();
    }

    static void lockBoth(Object first, Object second) {
        synchronized (first) {
            sleep(300);
            synchronized (second) {
                sleepForever();
            }
        }
    }

    static void waitForever() {
        Object monitor = new Object();
        synchronized (monitor) {
            try {
                monitor.wait();
            } catch (InterruptedException e) {
                Thread.currentThread().interrupt();
            }
        }
    }

    static void sleepForever() {
        while (true) {
            sleep(1000);
        }
    }

    static void sleep(long ms) {
        try {
            Thread.sleep(ms);
        } catch (InterruptedException e) {
            Thread.currentThread().interrupt();
        }
    }
}
