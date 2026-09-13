import module java.base;

public class Orders {
    sealed interface Order permits Paid, Refund, Cancel {}
    record Paid(long id, String sku, int qty, double total) implements Order {}
    record Refund(long id, double amount, String reason) implements Order {}
    record Cancel(long id) implements Order {}

    static final ScopedValue<Long> REQUEST = ScopedValue.newInstance();
    static final BlockingQueue<Order> QUEUE = new ArrayBlockingQueue<>(1_000);
    static final LongAdder PROCESSED = new LongAdder();

    public static void start() {
        Thread.ofPlatform().name("order-producer").daemon(true).start(Orders::produce);
        ThreadFactory workers = Thread.ofPlatform().name("order-worker-", 1).daemon(true).factory();
        for (int i = 0; i < 4; i++) {
            workers.newThread(Orders::consume).start();
        }
    }

    static void produce() {
        var random = new Random(42);
        for (long id = 1; ; id++) {
            Order order = switch ((int) (id % 5)) {
                case 0 -> new Refund(id, random.nextDouble(100), "damaged");
                case 1 -> new Cancel(id);
                default -> new Paid(id, "SKU-" + random.nextInt(50), 1 + random.nextInt(4), random.nextDouble(500));
            };
            try {
                QUEUE.put(order);
                Thread.sleep(15);
            } catch (InterruptedException e) {
                return;
            }
        }
    }

    static void consume() {
        while (true) {
            try {
                Order order = QUEUE.take();
                ScopedValue.where(REQUEST, order.hashCode() & 0xffffL).run(() -> handle(order));
                PROCESSED.increment();
            } catch (InterruptedException e) {
                return;
            }
        }
    }

    static void handle(Order order) {
        switch (order) {
            case Paid(long id, String sku, int qty, double total) when total > 400 -> fraudCheck(id, sku, qty);
            case Paid paid -> Hashing.sha256(paid.toString(), 2_000);
            case Refund refund -> pause(10);
            case Cancel cancel -> pause(2);
        }
    }

    static void fraudCheck(long id, String sku, int qty) {
        Hashing.sha256(REQUEST.get() + ":" + id + ":" + sku + ":" + qty, 20_000);
    }

    static void pause(long ms) {
        try {
            Thread.sleep(ms);
        } catch (InterruptedException e) {
            Thread.currentThread().interrupt();
        }
    }
}
