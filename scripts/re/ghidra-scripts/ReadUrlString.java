import ghidra.app.script.GhidraScript;
import ghidra.program.model.address.Address;

public class ReadUrlString extends GhidraScript {
    @Override
    public void run() throws Exception {
        Address addr = toAddr(0x102eb20a0L);
        byte[] b = new byte[0x1f];
        for (int i = 0; i < 0x1f; i++) {
            b[i] = getByte(addr.add(i));
        }
        println("String at 0x102eb20a0: " + new String(b));
    }
}
