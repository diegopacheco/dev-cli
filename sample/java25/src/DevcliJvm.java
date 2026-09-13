void main() throws InterruptedException {
    Orders.start();
    HttpTraffic.start();
    Deadlocks.start();
    Contention.start();
    Background.start();
    IO.println("devcli java " + Runtime.version().feature() + " sample running, pid " + ProcessHandle.current().pid());
    new CountDownLatch(1).await();
}
