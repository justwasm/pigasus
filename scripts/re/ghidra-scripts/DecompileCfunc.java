import ghidra.app.script.GhidraScript;
import ghidra.program.model.address.Address;
import ghidra.program.model.symbol.Symbol;
import ghidra.program.model.listing.Function;
import ghidra.app.decompiler.DecompInterface;
import ghidra.app.decompiler.DecompileResults;

public class DecompileCfunc extends GhidraScript {
    @Override
    public void run() throws Exception {
        Address addr = toAddr(0x102384720L);
        Symbol sym = currentProgram.getSymbolTable().getPrimarySymbol(addr);
        println("Symbol at 0x102384720: " + (sym != null ? sym.getName() : "null"));
        Function f = getFunctionAt(addr);
        if (f == null) f = createFunction(addr, "target_func");
        DecompInterface decomp = new DecompInterface();
        decomp.openProgram(currentProgram);
        DecompileResults res = decomp.decompileFunction(f, 30, monitor);
        if (res != null && res.getDecompiledFunction() != null) {
            println(res.getDecompiledFunction().getC());
        }
    }
}
