import ghidra.app.script.GhidraScript;
import ghidra.program.model.listing.Function;
import ghidra.program.model.listing.FunctionIterator;

public class FindStartRC extends GhidraScript {
    @Override
    public void run() throws Exception {
        FunctionIterator iter = currentProgram.getFunctionManager().getFunctions(true);
        while (iter.hasNext()) {
            Function f = iter.next();
            if (f.getName().toLowerCase().contains("startremotecontrol")) {
                println(String.format("0x%x: %s", f.getEntryPoint().getOffset(), f.getName()));
            }
        }
    }
}
