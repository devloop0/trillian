#!/usr/bin/env python3

from datagen import error_and_exit
from enum import Enum
import numpy as np
import sys, datagen, argparse, signal, os

"""
    File used to generate a series of transactions to test Trillium on.
    Each transaction refers to either a read or a write. A read leads
    to only reading from a single leaf node while a write specifies a
    user and public key pair and updates all the leaves containing that
    identifier.
"""

OUTPUT_PATH = "./"

INIT_FILE = "/init_tree"

CLIENT_FILE = "/transaction_client"

TRILLIAN_FILE = "/trillian_client"

NUM_PARTITIONS = 20 #Number of distinct files that make requests to the tree at one time.
                    #For simplicity we assume that each user has a unique portion of the
                    #tree and therefore will never have any conflicts

NUM_TRANSACTIONS = 1000 #Number of transactions to run at a given time for each user.


"""
    Enum class used to distinguish between transactions which READ
    and transactions which WRITE.
"""
class TransactionType (Enum):
    ERROR = 0
    READ = 1
    WRITE = 2

class WriteProbability (Enum):
    VERY_LOW = 0
    LOW = 1
    MEDIUM = 2
    HIGH = 3
    VERY_HIGH = 4

CURR_PROB = WriteProbability.VERY_LOW

#Dict with probability for each setting
WRITE_DICT = {WriteProbability.VERY_LOW: 0.05, WriteProbability.LOW: .15, WriteProbability.MEDIUM: .3, WriteProbability.HIGH: .55, WriteProbability.VERY_HIGH: .8}

CURR_STATE = TransactionType.READ #Variable that tells which Markov state the
                                  #system is currently in. The two states are
                                  #read in which the most recent transaction
                                  #was a read and makes further reads more
                                  #likely or a write which allows for
                                  #simulating bursty write behavior.

IS_BURSTY = False # Determines if writes should be very high when in the write state 

WRITE_PROBABILITY =  [WRITE_DICT[WriteProbability.VERY_LOW], WRITE_DICT[WriteProbability.VERY_LOW]]  #Percentage of transactions that should be
                                 #writes for each of the two states. The first
                                 #state is the read state and the second is
                                 #the write state.

def set_write_probabilities ():
    WRITE_PROBABILITY[TransactionType.READ.value - 1] = WRITE_DICT[CURR_PROB]
    WRITE_PROBABILITY[TransactionType.WRITE.value - 1] = WRITE_DICT[CURR_PROB]


def set_bursty ():
    WRITE_PROBABILITY[TransactionType.WRITE.value - 1] = WRITE_DICT[WriteProbability.VERY_HIGH]

def get_write_probability ():
    return WRITE_PROBABILITY [CURR_STATE.value - 1]

def update_state (state):
    CURR_STATE = state

def reset_state ():
    CURR_STATE = TransactionType.READ

def get_users_range (partition_number):
    mod_size = datagen.USER_COUNT % NUM_PARTITIONS
    div_size = datagen.USER_COUNT // NUM_PARTITIONS
    if partition_number < mod_size:
        lower_add = mod_size - partition_number
    else:
        lower_add = mod_size
    lower = partition_number * div_size + lower_add
    upper = lower + div_size -1 + (1 if partition_number < mod_size else 0)
    return lower, upper

"""
    Class used to hold the basic information for a transaction.
"""
class Transaction:

    """
        Constructor used to generate a random transaction for a tree.
    """
    def __init__ (self, t=None, partition_number=None):
        self.type = TransactionType.ERROR
        self.trillian_additions = []
        if t is not None and partition_number is not None:
            self.tid = t.get_id ()
            self.type = TransactionType.ERROR
            self.set_transaction_type ()
            i, j = get_users_range (partition_number)
            self.user, self.oldpk, self.id = t.get_random_leaf (i, j)
            if self.type == TransactionType.WRITE:
                self.newpk = datagen.random_64s (datagen.KEY_SIZE)
                self.trillian_additions = t.update_pk (self.user, self.oldpk, self.newpk)
            else:
                self.newpk = ""

    """
        Constructor used to generate a write transaction for a particular leaf.
        This transaction is associated with creating a leaf.
    """
    def configure_write_transaction (self, tid, user_id, pk, identifier):
        self.tid = tid
        self.user = user_id
        self.oldpk = ""
        self.id = identifier
        self.type = TransactionType.WRITE
        self.newpk = pk


    """
        Prints a transaction in form that can be read by a user.
    """
    def print_transaction (self):
        if self.type == TransactionType.ERROR:
            error_and_exit ("Invalid transaction type.")
        else:
            if self.type == TransactionType.READ:
                print ("Read to: ", end="")
            else:
                print ("Write to: ", end="")
            print ("(User: {}, with Public Key: {}, and Identifier: {})".format (self.user, self.pk, self.id))

    """
        Prints a transaction in a form that can be viewed as a database table.
        The table is a form of comma separated values in the form:

                       TYPE | TID | USER | OLDPK | IDENTIFIER | NEWPK

        where the TYPE is 1 for read and 2 for write.

        OLDPK is "" for any write which creates a leaf.

        NEWPK is "" for all reads.
    """
    def print_transaction_as_row (self, f):
        if self.type == TransactionType.ERROR:
            error_and_exit ("Invalid transaction type.")
        else:
            f.write ("{},{},{},{},{},{}\n".format (self.type.value, self.tid, self.user, self.oldpk, self.id, self.newpk))


    def print_trillian_transaction_as_row (self, f):
        if self.type == TransactionType.ERROR:
            error_and_exit ("Invalid transaction type.")
        elif self.type == TransactionType.READ:
            f.write ("{},{},{},{},{},{}\n".format (self.type.value, self.tid, self.user, self.oldpk, self.id, self.newpk))
        else:
            for device_id in self.trillian_additions:
                f.write ("{},{},{},{},{},{}\n".format (self.type.value, self.tid, self.user, self.oldpk, device_id, self.newpk))





    """
        Generates a random number and decides if the transaction
        is a read or a write.
    """
    def set_transaction_type (self):
        val = np.random.random ()
        prob = get_write_probability ()
        if val < prob:
            self.type = TransactionType.WRITE
        else:
            self.type = TransactionType.READ
        update_state (self.type)


"""
    Funciton used to generate a list of transactions for initializing the
    current tree t.
"""
def generate_init_transaction_list (t):
    trxns = []
    users = t.get_users ()
    for user in users:
        nodes = user.get_nodes ()
        for node in nodes:
            trxn = Transaction()
            trxn.configure_write_transaction (t.get_id (), user.get_id (), node.get_key (), node.get_id ())
            trxns.append (trxn)
    return trxns

"""
    Function used to generate a list of transactions to test concurrency on.
"""
def generate_transaction_list (t, partition_number):
    return [Transaction (t, partition_number) for c in range (NUM_TRANSACTIONS)]

"""
    Sample main function which will construct a sample set of transactions
    and print it.
"""
def main (tid):
    t = datagen.Tree (tid)
    init_trxns = generate_init_transaction_list (t)
    #trxns = generate_transaction_list (t)
    with open(OUTPUT_PATH + INIT_FILE, "w") as f:
        for trxn in init_trxns:
            trxn.print_transaction_as_row (f)
    for i in range (NUM_PARTITIONS):
        reset_state ()
        trxns = generate_transaction_list (t, i)
        with open(OUTPUT_PATH + CLIENT_FILE + str(i), "w") as f:
            for trxn in trxns:
                trxn.print_transaction_as_row (f)
        with open(OUTPUT_PATH + TRILLIAN_FILE + str(i), "w") as f:
            for trxn in trxns:
                trxn.print_trillian_transaction_as_row (f)


def handler (signum, frame):
    print ("Exiting due to interupt")
    exit (1)

if __name__ == '__main__':
    signal.signal (signal.SIGUSR1, handler)
    parser = argparse.ArgumentParser (description="Initialize a trillian tree \
            and initialize a series of transactions for many clients to simultaneously \
            run on the tree.")
    parser.add_argument ("--bursty", dest="bursty", nargs="?", type=bool, const=True,
            default=False, help="Determines if transactions to the write state \
            should result in many consecutive writes. Burstiness will not carry \
            over across users.")
    parser.add_argument ("--num_partitions", nargs="?", type=int, dest="num_partitions",
            const=True, default=20, help="Determines how many concurrent clients should hammer \
                    the tree.")
    parser.add_argument ("--output_path", nargs="?", type=str, dest="output_path",
            const=True, default="./", help="Determines the directory to which any output \
                    files should be written.")
    parser.add_argument ("--num_users", nargs="?", type=int, dest="num_users",
            const=True, default=1000, help="Determines how many unique users should \
            be in the tree.")
    parser.add_argument ("--lambda", nargs="?", type=int, dest="_lambda",
            const=True, default=10, help="Determines the average number of \
                    devices each user should have.")
    parser.add_argument ("--lambda_pk", nargs="?", type=int, dest="lambda_pk",
            const=True, default=4, help="Determines the average number of \
                    pks each user should have.")
    parser.add_argument ("--write_frequency", nargs="?", type=str, dest="write_frequency",
            const=True, default="vl", help="Determines how often writes should occur.")
    parser.add_argument ("tid", type=int, help="The id for the tree being operated on.")
    items = parser.parse_args ()
    if items.write_frequency == "l":
        CURR_PROB = WriteProbability.LOW
    elif items.write_frequency == "m":
        CURR_PROB = WriteProbability.MEDIUM
    elif items.write_frequency == "h":
        CURR_PROB = WriteProbability.HIGH
    elif items.write_frequency == "vh":
        CURR_PROB = WriteProbability.VERY_HIGH
    else:
        CURR_PROB = WriteProbability.VERY_LOW

    set_write_probabilities ()

    IS_BURSTY = items.bursty
    if IS_BURSTY:
        set_bursty ()
    NUM_PARTITIONS = items.num_partitions
    OUTPUT_PATH = items.output_path
    if not os.path.exists (OUTPUT_PATH):
        error_and_exit ("Invalid path destination. Script will not create directories")
    datagen.USER_COUNT = items.num_users
    datagen.LAMBDA = items._lambda
    datagen.LAMBDA_PK = items.lambda_pk
    if items.num_users < items.num_partitions:
        error_and_exit ("Too many partitions for given number of users")
    main (items.tid)
