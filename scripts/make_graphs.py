#!/usr/bin/env python3
import matplotlib.pyplot as plt
import matplotlib as mpl

# First graph
g1 = False
# Second graph
g2 = False
# Third graph
g3 = True
# Fourth graph
g4 = True

if g1:
    plt.bar (x=3 - 2, width=1, height=19, color='blue')
    plt.bar (x=3 - 1, width=1, height=17, color='green')
    plt.bar (x=3, width=1, height=15, color='yellow')
    plt.bar (x=3 + 1, width=1, height=55, color='orange')
    plt.bar (x=3 + 2, width=1, height=55, color='brown')

    plt.bar (x=9-2, width=1, height=20, color='blue')
    plt.bar (x=9-1, width=1, height=21, color='green')
    plt.bar (x=9, width=1, height=33, color='yellow')
    plt.bar (x=9+1, width=1, height=34, color='orange')
    plt.bar (x=9+2, width=1, height=60, color='brown')
    plt.xticks([3, 9])
    plt.yticks(range(0, 61, 10))
    plt.xlabel("Average Transaction Size (Number Leaves)", fontsize=28)
    plt.ylabel("Average Time to Finish Adding all Leaves\n(Seconds)", fontsize=24)

    plt.rcParams["legend.fontsize"] = 20
    patches = []
    patches.append (mpl.patches.Patch (color='blue', label='write_probability=0.05 and not bursty'))
    patches.append (mpl.patches.Patch (color='green', label='write_probability=0.05 and bursty'))
    patches.append (mpl.patches.Patch (color='yellow', label='write_probability=0.3 and not bursty'))
    patches.append (mpl.patches.Patch (color='orange', label='write_probability=0.3 and bursty'))
    patches.append (mpl.patches.Patch (color='brown', label='write_probability=0.8'))
    plt.legend(handles=patches, loc="upper left")
    plt.title ("Time to Populate Tree Per Transaction Size for Trillian", fontsize=34)
    plt.show ()

    plt.bar (x=3-2, width=1, height=17, color='blue')
    plt.bar (x=3-1, width=1, height=16, color='green')
    plt.bar (x=3, width=1, height=22, color='yellow')
    plt.bar (x=3+1, width=1, height=22, color='orange')
    plt.bar (x=3+2, width=1, height=30, color='brown')

    plt.bar (x=9-2, width=1, height=19, color='blue')
    plt.bar (x=9-1, width=1, height=18, color='green')
    plt.bar (x=9, width=1, height=27, color='yellow')
    plt.bar (x=9+1, width=1, height=26, color='orange')
    plt.bar (x=9+2, width=1, height=42, color='brown')
    plt.xticks([3, 9])
    plt.yticks(range(0, 61, 10))
    plt.xlabel("Average Transaction Size (Number Leaves)", fontsize=28)
    plt.ylabel("Average Time to Finish Adding all Leaves\n(Seconds)", fontsize=24)
    patches = []
    patches.append (mpl.patches.Patch (color='blue', label='write_probability=0.05 and not bursty'))
    patches.append (mpl.patches.Patch (color='green', label='write_probability=0.05 and bursty'))
    patches.append (mpl.patches.Patch (color='yellow', label='write_probability=0.3 and not bursty'))
    patches.append (mpl.patches.Patch (color='orange', label='write_probability=0.3 and bursty'))
    patches.append (mpl.patches.Patch (color='brown', label='write_probability=0.8'))
    plt.legend(handles=patches, loc="upper left")
    plt.rcParams["legend.fontsize"] = 20
    plt.title ("Time to Populate Tree Per Transaction Size for Modified Trillian", fontsize=34)
    plt.show ()
if g2:
    plt.bar (x=3 - 2, width=1, height=46.2105263, color='blue')
    plt.bar (x=3 - 1, width=1, height=45.2352941, color='green')
    plt.bar (x=3, width=1, height=331.133333, color='yellow')
    plt.bar (x=3 + 1, width=1, height=92.6727273, color='orange')
    plt.bar (x=3 + 2, width=1, height=240.690909, color='brown')

    plt.bar (x=9-2, width=1, height=49.8823529, color='blue')
    plt.bar (x=9-1, width=1, height=51.875, color='green')
    plt.bar (x=9, width=1, height=233.363636, color='yellow')
    plt.bar (x=9+1, width=1, height=233.636364, color='orange')
    plt.bar (x=9+2, width=1, height=446.066667, color='brown')
    plt.xticks([3, 9])
    plt.yticks(range(0, 900, 100))
    plt.xlabel("Average Transaction Size (Number Leaves)", fontsize=28)
    plt.ylabel("Average Throughput\n(Leaves Added per Second)", fontsize=24)

    plt.rcParams["legend.fontsize"] = 20
    patches = []
    patches.append (mpl.patches.Patch (color='blue', label='write_probability=0.05 and not bursty'))
    patches.append (mpl.patches.Patch (color='green', label='write_probability=0.05 and bursty'))
    patches.append (mpl.patches.Patch (color='yellow', label='write_probability=0.3 and not bursty'))
    patches.append (mpl.patches.Patch (color='orange', label='write_probability=0.3 and bursty'))
    patches.append (mpl.patches.Patch (color='brown', label='write_probability=0.8'))
    plt.legend(handles=patches, loc="upper left")
    plt.title ("Throughput Per Transaction Size for Trillian", fontsize=34)
    plt.show ()

    plt.bar (x=3-2, width=1, height=93.7, color='blue')
    plt.bar (x=3-1, width=1, height=93.1428571, color='green')
    plt.bar (x=3, width=1, height=323.515152, color='yellow')
    plt.bar (x=3+1, width=1, height=315.647059, color='orange')
    plt.bar (x=3+2, width=1, height=481.75, color='brown')

    plt.bar (x=9-2, width=1, height=101.210526, color='blue')
    plt.bar (x=9-1, width=1, height=94.8888889, color='green')
    plt.bar (x=9, width=1, height=418.518519, color='yellow')
    plt.bar (x=9+1, width=1, height=393.769231, color='orange')
    plt.bar (x=9+2, width=1, height=731.142857, color='brown')
    plt.xticks([3, 9])
    plt.yticks(range(0, 900, 100))
    plt.xlabel("Average Transaction Size (Number Leaves)", fontsize=28)
    plt.ylabel("Average Throughput\n(Leaves Added per Second)", fontsize=24)
    patches = []
    patches.append (mpl.patches.Patch (color='blue', label='write_probability=0.05 and not bursty'))
    patches.append (mpl.patches.Patch (color='green', label='write_probability=0.05 and bursty'))
    patches.append (mpl.patches.Patch (color='yellow', label='write_probability=0.3 and not bursty'))
    patches.append (mpl.patches.Patch (color='orange', label='write_probability=0.3 and bursty'))
    patches.append (mpl.patches.Patch (color='brown', label='write_probability=0.8'))
    plt.legend(handles=patches, loc="upper left")
    plt.title ("Throughput Per Transaction Size for Modified Trillian", fontsize=34)
    plt.show ()
if g3:
    #plt.rcParams["legend.fontsize"] = 20
    patches = []
    patches.append (mpl.patches.Patch (color='blue',  label='Modified Trillian'))
    patches.append (mpl.patches.Patch (color='green',  label='Original Trillian'))
    plt.legend(handles=patches, loc="upper left")
    plt.plot([5, 10, 20, 40, 80], [0.52, 1.24, 2.96, 3.45, 6.55], 'o-',color='blue')
    plt.plot([5, 10, 20, 40, 80], [0.27, 0.68, 1.59, 2.98, 5.02], 'o-',color='green')
    plt.xlabel("Number of Clients")
    plt.ylabel("Average Leaf Latency (s)")
    plt.title ("Average Signer Latency per Number of Clients")
    plt.savefig ("SignerLatency.pdf", bbox_inches="tight")
    plt.show ()
if g4:
    #plt.rcParams["legend.fontsize"] = 20
    patches = []
    patches.append (mpl.patches.Patch (color='blue',  label='Modified Trillian'))
    patches.append (mpl.patches.Patch (color='green',  label='Original Trillian'))
    plt.legend(handles=patches, loc="upper right")
    plt.plot([5, 10, 20, 40, 80], [242.636364, 225.652174, 194.792079, 179.358283, 167.282946], 'o-',color='blue')
    plt.plot([5, 10, 20, 40, 80], [229.576923, 203.591837, 175.061947, 154.289382, 140.078393], 'o-',color='green')
    plt.xlabel("Number of Clients")
    plt.ylabel("Average Throughput (Leaves Added per Second)")
    plt.title ("Average Throughput per Number of Clients")
    plt.savefig ("ThroughputClient.pdf", bbox_inches="tight")
    #plt.show ()
