// DecompileAt.java — decompile the functions at the addresses given as
// script arguments. Each argument is a hex address (0x…); the script
// prints the primary symbol at that address and the decompiled C.
//
//   analyzeHeadless <proj> agy -process -noanalysis -max-cpu 4 \
//     -scriptPath <dir> -postScript DecompileAt.java 0x102bb1200 0x102bb1bf0
//
// The function must already exist in the program (the seeds applied it);
// otherwise createFunction is attempted and the decompile may fail.
import ghidra.app.script.GhidraScript;
import ghidra.program.model.address.Address;
import ghidra.program.model.symbol.Symbol;
import ghidra.program.model.listing.Function;
import ghidra.app.decompiler.DecompInterface;
import ghidra.app.decompiler.DecompileResults;

public class DecompileAt extends GhidraScript {
    @Override
    public void run() throws Exception {
        DecompInterface decomp = new DecompInterface();
        decomp.openProgram(currentProgram);
        for (String a : getScriptArgs()) {
            String t = a.trim();
            if (t.isEmpty()) {
                continue;
            }
            Address addr = toAddr(Long.decode(t));
            Symbol sym = currentProgram.getSymbolTable().getPrimarySymbol(addr);
            println("=== " + t + "  " + (sym != null ? sym.getName() : "<no symbol>"));
            Function f = getFunctionAt(addr);
            if (f == null) {
                f = getFunctionContaining(addr);
                if (f != null) {
                    println("  (inside " + f.getName() + " at 0x" + Long.toHexString(f.getEntryPoint().getOffset()) + ")");
                } else {
                    f = createFunction(addr, "target_" + t);
                }
            }
            if (f == null) {
                println("  no function at " + t);
                continue;
            }
            DecompileResults res = decomp.decompileFunction(f, 60, monitor);
            if (res != null && res.getDecompiledFunction() != null) {
                println(res.getDecompiledFunction().getC());
            } else {
                println("  decompile failed: " + (res == null ? "null result" : res.getErrorMessage()));
            }
        }
        decomp.dispose();
    }
}
