import ghidra.app.script.GhidraScript;
import ghidra.program.model.address.Address;
import ghidra.program.model.symbol.Symbol;

public class FindVtables extends GhidraScript {
    @Override
    public void run() throws Exception {
        long[] addrs = new long[] {
            0x106880ea8L, 0x106880f08L, 0x106880d38L, 0x106880cd8L
        };
        for (long a : addrs) {
            Address addr = toAddr(a);
            Symbol sym = currentProgram.getSymbolTable().getPrimarySymbol(addr);
            println(String.format("0x%x -> %s", a, sym != null ? sym.getName() : "null"));
        }
    }
}
