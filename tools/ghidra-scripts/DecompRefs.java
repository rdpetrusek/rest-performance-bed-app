// Ghidra headless post-script: decompiles functions of interest.
// Args: each arg is either a substring to match against defined strings
// (every function referencing a matching string is decompiled), or
// "fn:<hex addr>" to decompile the function containing that address, or
// "callers:<hex addr>" to list and decompile that function's callers.
import ghidra.app.decompiler.DecompInterface;
import ghidra.app.decompiler.DecompileResults;
import ghidra.app.script.GhidraScript;
import ghidra.program.model.address.Address;
import ghidra.program.model.listing.Data;
import ghidra.program.model.listing.Function;
import ghidra.program.model.symbol.Reference;

import java.util.LinkedHashSet;
import java.util.Set;

public class DecompRefs extends GhidraScript {
    private DecompInterface decomp;
    private final Set<Function> done = new LinkedHashSet<>();

    @Override
    public void run() throws Exception {
        decomp = new DecompInterface();
        decomp.openProgram(currentProgram);
        for (String arg : getScriptArgs()) {
            if (arg.startsWith("mkfn:")) {
                // Code reached only through a pointer (e.g. an RTOS task entry)
                // that auto-analysis never made a function out of.
                Address a = toAddr(Long.parseLong(arg.substring(5), 16));
                Function f = getFunctionAt(a);
                if (f == null) {
                    disassemble(a);
                    f = createFunction(a, null);
                }
                emit(f, arg);
            } else if (arg.startsWith("fn:")) {
                emit(fnAt(arg.substring(3)), arg);
            } else if (arg.startsWith("callers:")) {
                Function f = fnAt(arg.substring(8));
                if (f == null) continue;
                for (Function c : f.getCallingFunctions(monitor)) {
                    emit(c, "caller of " + f.getName());
                }
            } else if (arg.startsWith("ptr:")) {
                // Literal-pool slot: print the address it holds, then decompile
                // every function that references that address (via any pool slot).
                Address slot = toAddr(Long.parseLong(arg.substring(4), 16));
                Address target = toAddr(getInt(slot) & 0xffffffffL);
                println("PTR " + slot + " -> " + target);
                refsTo(target, "uses " + target);
                // Globals in .bss have no memory block, so Ghidra makes no refs
                // to them; find every pool slot holding the same pointer instead.
                int v = getInt(slot);
                byte[] le = {(byte) v, (byte) (v >> 8), (byte) (v >> 16), (byte) (v >> 24)};
                Address hit = currentProgram.getMinAddress();
                while ((hit = find(hit, le)) != null) {
                    for (Reference r : getReferencesTo(hit)) {
                        emit(getFunctionContaining(r.getFromAddress()), "uses " + target);
                    }
                    hit = hit.add(1);
                }
            } else if (arg.startsWith("ref:")) {
                refsTo(toAddr(Long.parseLong(arg.substring(4), 16)), arg);
            } else if (arg.startsWith("mem:")) {
                // mem:<slot or addr>:<words>[:deref]
                String[] p = arg.split(":");
                Address a = toAddr(Long.parseLong(p[1], 16));
                if (p.length > 3) a = toAddr(getInt(a) & 0xffffffffL);
                StringBuilder sb = new StringBuilder("MEM " + a + ":");
                for (int i = 0; i < Integer.parseInt(p[2]); i++) {
                    sb.append(String.format(" %08x", getInt(a.add(i * 4))));
                }
                println(sb.toString());
            } else if (arg.startsWith("findptr:")) {
                // Find a raw string, then every literal-pool slot holding its
                // address, then the functions that load that slot.
                byte[] needle = arg.substring(8).getBytes();
                Address hit = currentProgram.getMinAddress();
                while ((hit = find(hit, needle)) != null) {
                    long a = hit.getOffset();
                    println("FOUND '" + arg.substring(8) + "' @ " + hit);
                    byte[] le = {(byte) a, (byte) (a >> 8), (byte) (a >> 16), (byte) (a >> 24)};
                    Address slot = currentProgram.getMinAddress();
                    while ((slot = find(slot, le)) != null) {
                        println("  pool slot " + slot);
                        for (Reference r : getReferencesTo(slot)) {
                            emit(getFunctionContaining(r.getFromAddress()), "loads " + arg.substring(8));
                        }
                        slot = slot.add(1);
                    }
                    hit = hit.add(1);
                }
            } else if (arg.startsWith("find:")) {
                byte[] needle = arg.substring(5).getBytes();
                Address hit = currentProgram.getMinAddress();
                while ((hit = find(hit, needle)) != null) {
                    println("FOUND '" + arg.substring(5) + "' @ " + hit);
                    refsTo(hit, "string " + arg.substring(5));
                    hit = hit.add(1);
                }
            } else {
                byString(arg);
            }
        }
    }

    private Function fnAt(String hex) {
        Address a = toAddr(Long.parseLong(hex.replace("0x", ""), 16));
        Function f = getFunctionContaining(a);
        if (f == null) {
            println("NO FUNCTION at " + hex);
        }
        return f;
    }

    private void byString(String needle) throws Exception {
        for (Data d : currentProgram.getListing().getDefinedData(true)) {
            Object v = d.getValue();
            if (v == null || !v.toString().contains(needle)) continue;
            println("STRING @ " + d.getAddress() + ": " + v);
            for (Reference r : getReferencesTo(d.getAddress())) {
                Function f = getFunctionContaining(r.getFromAddress());
                if (f == null) {
                    // Literal-pool entry: follow refs to the pool slot.
                    for (Reference r2 : getReferencesTo(r.getFromAddress())) {
                        emit(getFunctionContaining(r2.getFromAddress()), "string " + v);
                    }
                    continue;
                }
                emit(f, "string " + v);
            }
        }
    }

    private void refsTo(Address target, String why) {
        for (Reference r : getReferencesTo(target)) {
            Function f = getFunctionContaining(r.getFromAddress());
            if (f != null) {
                emit(f, why);
                continue;
            }
            for (Reference r2 : getReferencesTo(r.getFromAddress())) {
                emit(getFunctionContaining(r2.getFromAddress()), why);
            }
        }
    }

    private void emit(Function f, String why) {
        if (f == null || !done.add(f)) return;
        println("\n==== " + f.getName() + " @ " + f.getEntryPoint() + "  (" + why + ")");
        StringBuilder callers = new StringBuilder();
        for (Function c : f.getCallingFunctions(monitor)) callers.append(c.getName()).append(' ');
        println("callers: " + callers);
        DecompileResults res = decomp.decompileFunction(f, 60, monitor);
        if (res.decompileCompleted()) {
            println(res.getDecompiledFunction().getC());
        } else {
            println("DECOMPILE FAILED: " + res.getErrorMessage());
        }
    }
}
