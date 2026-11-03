import ghidra.app.script.GhidraScript;
import ghidra.program.model.address.Address;
import ghidra.program.model.symbol.Symbol;

public class PrintPointers extends GhidraScript {
    @Override
    public void run() throws Exception {
        long[] addrs = new long[] {
            0x109817c28L, 0x109817c30L, 0x109817c38L, 0x109817c40L, 0x109817c48L, 0x109817c50L, 0x109817c58L, 0x109817c60L
        };
        for (long a : addrs) {
            Address addr = toAddr(a);
            long val = getLong(addr);
            Address targetAddr = toAddr(val);
            Symbol sym = currentProgram.getSymbolTable().getPrimarySymbol(targetAddr);
            println(String.format("0x%x -> 0x%x (%s)", a, val, sym != null ? sym.getName() : "no sym"));
        }
    }
}
