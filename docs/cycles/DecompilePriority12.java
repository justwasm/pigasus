//DecompilePriority12.java - Ghidra postScript for cycle 41 retry of 12 ERR functions
//Targets the 12 backend functions that timed out at default 60s in cycle 40.
//Per-function timeout: 300 sec (5 min) — Ghidra 12.1.4 decompiler limit on huge funcs.
//
//Usage:
//  analyzeHeadless <project> <name> \
//    -import <binary> -Xmx2500M -noanalysis \
//    -preScript ReaGhidraApplySeeds.java <priority-12.txt> \
//    -postScript DecompilePriority12.java <priority-12.txt> <out_dir>
//    -scriptPath <dir>
//
//Format of priority file: 0xADDR\tfunction\tNAME  (tab-separated)
//Output naming: <hex>_<safe_name>.c (e.g. 0877af80_GetSelectedModelOverride_deferwrap1.c)

import ghidra.app.script.GhidraScript;
import ghidra.app.decompiler.DecompInterface;
import ghidra.app.decompiler.DecompileResults;
import ghidra.app.decompiler.DecompiledFunction;
import ghidra.app.decompiler.DecompileOptions;
import ghidra.program.model.listing.Function;
import ghidra.program.model.listing.FunctionManager;
import ghidra.program.model.address.Address;
import ghidra.program.model.address.AddressFactory;
import java.io.*;

public class DecompilePriority12 extends GhidraScript {

    private static final int DECOMPILE_TIMEOUT_SEC = 120; // 5 min per func

    private String priorityFile;
    private String outDir;

    private static String sanitize(String name) {
        int lastDot = name.lastIndexOf('.');
        String stem = lastDot >= 0 ? name.substring(lastDot + 1) : name;
        StringBuilder sb = new StringBuilder();
        for (int i = 0; i < stem.length(); i++) {
            char c = stem.charAt(i);
            if ((c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9')) {
                sb.append(c);
            } else {
                sb.append('_');
            }
        }
        if (sb.length() > 80) sb.setLength(80);
        return sb.toString();
    }

    private static String addrHex(String addrTok) {
        if (addrTok.startsWith("0x") || addrTok.startsWith("0X")) {
            return addrTok.substring(2);
        }
        return addrTok;
    }

    public void run() throws Exception {
        String[] args = getScriptArgs();
        if (args.length < 2) {
            println("DecompilePriority12: usage <priority_file> <out_dir>");
            return;
        }
        priorityFile = args[0];
        outDir = args[1];

        File outDirFile = new File(outDir);
        if (!outDirFile.exists()) {
            outDirFile.mkdirs();
        }
        File progressFile = new File(outDir, "_progress.txt");
        File errorFile = new File(outDir, "_errors.txt");
        File summaryFile = new File(outDir, "_summary.txt");
        // Truncate errorFile and summaryFile on each run (separate from cycle 40 errors).
        new FileWriter(errorFile, false).close();
        new FileWriter(summaryFile, false).close();
        new FileWriter(progressFile, false).close();

        int totalProcessed = 0;
        int totalDecompiled = 0;
        int totalSkipped = 0;
        int totalErrors = 0;
        int totalTimeout = 0;
        int totalSuccess = 0;

        PrintWriter errPw = new PrintWriter(new FileWriter(errorFile, true));
        PrintWriter progPw = new PrintWriter(new FileWriter(progressFile, true));
        PrintWriter sumPw = new PrintWriter(new FileWriter(summaryFile, true));
        long startMs = System.currentTimeMillis();

        println("DecompilePriority12: opening decompiler with 300s/func timeout...");
        DecompInterface decomp = new DecompInterface();
        // Set decompiler options: increase max-size payload (large funcs).
        DecompileOptions opts = new DecompileOptions();
        opts.grabFromProgram(currentProgram);
        opts.setMaxPayloadMBytes(0x500); // 1.25 GB (default 256MB insufficient for huge Go funcs)
        opts.setMaxInstructions(Integer.MAX_VALUE);
        opts.setDefaultTimeout(DECOMPILE_TIMEOUT_SEC);
        decomp.setOptions(opts);
        decomp.openProgram(currentProgram);
        println("DecompilePriority12: decompiler ready.");
        println("DecompilePriority12: priority file = " + priorityFile);
        println("DecompilePriority12: out dir = " + outDir);

        FunctionManager funcMgr = currentProgram.getFunctionManager();
        AddressFactory af = currentProgram.getAddressFactory();

        BufferedReader br = new BufferedReader(new FileReader(priorityFile));
        String line;
        int lineNum = 0;
        while ((line = br.readLine()) != null && totalProcessed < 100000) {
            lineNum++;
            line = line.trim();
            if (line.isEmpty() || line.startsWith("#")) continue;
            String[] parts = line.split("\t");
            if (parts.length < 3) {
                errPw.println("SKIP\tmalformed line " + lineNum + ": " + line);
                errPw.flush();
                totalSkipped++;
                continue;
            }
            String addrTok = parts[0];
            String kind = parts[1];
            String name = parts[2];

            if (!kind.equals("function")) {
                errPw.println("SKIP\tnot a function: " + line);
                errPw.flush();
                totalSkipped++;
                continue;
            }

            String hex = addrHex(addrTok);
            Address addr;
            try {
                addr = af.getAddress("0x" + hex);
            } catch (Exception e) {
                errPw.println("SKIP\tbad address: " + addrTok + " (" + e.getMessage() + ")");
                errPw.flush();
                totalSkipped++;
                continue;
            }

            Function func = funcMgr.getFunctionAt(addr);
            if (func == null) {
                errPw.println("SKIP\tno function at " + addrTok + " (" + name + ")");
                errPw.flush();
                totalSkipped++;
                continue;
            }

            String safe = sanitize(name);
            String outName = String.format("%s_%s.c", hex, safe);
            File outFile = new File(outDir, outName);
            if (outFile.exists() && outFile.length() > 100) {
                println("DecompilePriority12: skip existing " + outName + " (" + outFile.length() + " bytes)");
                totalProcessed++;
                totalDecompiled++;
                totalSuccess++;
                continue;
            }

            long funcStart = System.currentTimeMillis();
            println(String.format("DecompilePriority12: [%d/%d] decompiling 0x%s (%s) with %ds timeout...",
                lineNum, 12, hex, name, DECOMPILE_TIMEOUT_SEC));
            progPw.println(String.format("START 0x%s %s %s", hex, name, new java.util.Date()));
            progPw.flush();

            try {
                DecompileResults res = decomp.decompileFunction(func, DECOMPILE_TIMEOUT_SEC, monitor);
                long funcElapsed = (System.currentTimeMillis() - funcStart) / 1000;
                totalProcessed++;
                if (res != null && res.decompileCompleted()) {
                    DecompiledFunction df = res.getDecompiledFunction();
                    if (df != null) {
                        String code = df.getC();
                        PrintWriter pw = new PrintWriter(new FileWriter(outFile));
                        pw.println("// Source: 0x" + hex + "  " + name);
                        pw.println("// Decompiled at: " + new java.util.Date());
                        pw.println("// Signature: " + func.getSignature().toString());
                        pw.println("// Timeout: " + DECOMPILE_TIMEOUT_SEC + " sec");
                        pw.println("// Wall: " + funcElapsed + " sec");
                        pw.println();
                        pw.print(code);
                        pw.close();
                        totalDecompiled++;
                        totalSuccess++;
                        println(String.format("DecompilePriority12: [%d/%d] OK 0x%s (%ds, %d bytes)",
                            lineNum, 12, hex, funcElapsed, code.length()));
                        progPw.println(String.format("OK 0x%s %d sec %d bytes", hex, funcElapsed, code.length()));
                    } else {
                        errPw.println("ERR\tnull decompiled function: 0x" + hex + " " + name);
                        totalErrors++;
                        println("DecompilePriority12: ERR null decompiled: 0x" + hex);
                    }
                } else {
                    String errMsg = (res != null && res.getErrorMessage() != null) ? res.getErrorMessage() : "decompile did not complete";
                    errPw.println("ERR\t" + errMsg + ": 0x" + hex + " " + name + " (" + funcElapsed + "s)");
                    totalErrors++;
                    if (errMsg.toLowerCase().contains("timeout")) totalTimeout++;
                    println(String.format("DecompilePriority12: ERR 0x%s %s (%ds)", hex, errMsg, funcElapsed));
                    progPw.println(String.format("ERR 0x%s %s %ds", hex, errMsg, funcElapsed));
                }
                progPw.flush();
                errPw.flush();
            } catch (Exception e) {
                long funcElapsed = (System.currentTimeMillis() - funcStart) / 1000;
                errPw.println("EXC\t" + e.getClass().getSimpleName() + ": " + e.getMessage() + ": 0x" + hex + " " + name + " (" + funcElapsed + "s)");
                totalErrors++;
                errPw.flush();
                println(String.format("DecompilePriority12: EXC 0x%s %s: %s (%ds)", hex, e.getClass().getSimpleName(), e.getMessage(), funcElapsed));
                if (monitor.isCancelled()) {
                    errPw.println("ABORT\tmonitor cancelled");
                    errPw.flush();
                    break;
                }
            }
        }
        br.close();

        long elapsedSec = (System.currentTimeMillis() - startMs) / 1000;
        sumPw.println("# DecompilePriority12 (cycle 41 retry) summary");
        sumPw.println("# Per-func timeout: " + DECOMPILE_TIMEOUT_SEC + " sec");
        sumPw.println("# Started:  " + new java.util.Date(startMs));
        sumPw.println("# Ended:    " + new java.util.Date());
        sumPw.println("# Elapsed:  " + elapsedSec + " sec (" + (elapsedSec / 60) + " min " + (elapsedSec % 60) + " sec)");
        sumPw.println("# Total processed: " + totalProcessed);
        sumPw.println("# Total decompiled OK: " + totalDecompiled);
        sumPw.println("# Total success: " + totalSuccess);
        sumPw.println("# Total errors: " + totalErrors);
        sumPw.println("# Total timeouts: " + totalTimeout);
        sumPw.println("# Total skipped: " + totalSkipped);
        sumPw.println("# Priority file: " + priorityFile);
        sumPw.println("# Output dir: " + outDir);
        sumPw.close();
        errPw.close();
        progPw.close();

        println(String.format("DecompilePriority12 DONE: %d processed, %d ok, %d err, %d skip in %d sec",
            totalProcessed, totalDecompiled, totalErrors, totalSkipped, elapsedSec));
    }
}
