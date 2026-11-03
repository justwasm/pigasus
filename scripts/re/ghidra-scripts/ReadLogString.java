import ghidra.app.script.GhidraScript;
import ghidra.program.model.address.Address;

public class ReadLogString extends GhidraScript {
    @Override
    public void run() throws Exception {
        Address addr = toAddr(0x102f131faL);
        byte[] b = new byte[0x2f];
        for (int i = 0; i < 0x2f; i++) {
            b[i] = getByte(addr.add(i));
        }
        println("Log format: " + new String(b));
    }
}
