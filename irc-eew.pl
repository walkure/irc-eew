#!/usr/bin/env perl

# irc-eew.pl
# charset = euc-jp
# walkure at 3pf.jp

use strict;
use warnings;
use IO::Select;
use YAML::Syck;
use Encode;
use Earthquake::EEW::Decoder;

use IRCSock;
use EEWSock;

my $path = $ARGV[0] || 'config.yaml';

print "++Loading YAML:$path\n";
my $yaml = YAML::Syck::LoadFile($path);

my $eewlog = $yaml->{logdir};

if(defined $eewlog){
	print "++LogDir:[$eewlog]\n";
}else{
	print "++LogDir:N/A not save EEW data\n";
}

my $select = IO::Select->new();
print "++Get WNIEEW server list\n";
my $serv_list = EEWSock->get_server_list();
my $eewsock;

do{
	print "++Connect WNIEEW Server\n";
	$eewsock = EEWSock->new(ServerList=>$serv_list,Callback=>\&eew_callback);
}until(defined $eewsock);

my $eewdec = Earthquake::EEW::Decoder->new();

$select->add($eewsock);
$eewsock->logon($yaml->{WNIEEW});

my $ircsock = IRCSock->new($yaml->{irc}{server});
$select->add($ircsock);

my (@join_ch,%channel_noticeall,%channel_noticelimited);

if(defined $yaml->{irc}{'all-notice'}){
	foreach my $ch(split(/,/,$yaml->{irc}{'all-notice'})){
		unless(defined $channel_noticeall{$ch} && defined $channel_noticelimited{$ch}){
			push(@join_ch,$ch);
			$channel_noticeall{$ch} = 1;
		}
	}
}

if(defined $yaml->{irc}{'limited-notice'}){
	foreach my $ch(split(/,/,$yaml->{irc}{'limited-notice'})){
		unless(defined $channel_noticeall{$ch} && defined $channel_noticelimited{$ch}){
			push(@join_ch,$ch);
			$channel_noticelimited{$ch} = 1;
		}
	}
}

$ircsock->login(\@join_ch);

while(1){
	foreach my $sock($select->can_read(undef)){
		my $buf;
		my $len = $sock->sysread($buf,65535);
		
		if($len){
			$sock->parse_body($buf);
		}else{
			#��³���ڤ줿
			print "++Connection closed...\n";
			$select->remove($sock);
			$sock->close();
			if(ref($sock) eq 'EEWSock'){
				do{
					$eewsock = EEWSock->new(ServerList => $serv_list,Callback=>\&eew_callback);
				}until(defined $eewsock);
				$select->add($eewsock);
				$eewsock->logon($yaml->{WNIEEW});
			}elsif(ref($sock) eq 'IRCSock'){
				$ircsock = IRCSock->new($yaml->{irc});
				$select->add($ircsock);
				$ircsock->login();
			}
		}
	}
}

my $last_eq_id = '';
sub eew_callback
{
	my($self,$md5,$buffer) = @_;
	
	my $tmpfname = time;
	$tmpfname .= ".$md5";
	if(defined $eewlog){
		print "+++Saved:$tmpfname\n";
		if(open(LOG,">$eewlog/$tmpfname")){
			print LOG $buffer;
			close LOG;
		}
	}
	
	my $d = $eewdec->read_data($buffer);
	conv_charset($d);

	my @wd = $d->{warn_time} =~ /\d\d/og;
	my @ed = $d->{eq_time}   =~ /\d\d/og;
	my $warnmsg = "20$wd[0]/$wd[1]/$wd[2] $wd[3]:$wd[4]:$wd[5]";
	my $eqedmsg = "20$ed[0]/$ed[1]/$wd[2] $ed[3]:$ed[4]:$ed[5]";

	my $times = ' ��'.$d->{'warn_num'}.'��';
	$times .= '(�ǽ�)' if $d->{NCN_type} > 0;

	my $msg;

	if($d->{'msg_type_code'} == 10){
		$msg = $warnmsg.$times.' ('.$eqedmsg.'ȯ��) �����ä����ޤ���';
	}else{
		my $center = sprintf('�̱�:N%.01f/E%0.01f(%s)����%dkm',$d->{'center_lat'},$d->{'center_lng'},$d->{'center_name'},$d->{'center_depth'});
		my $magnitude = sprintf(' ����:M%.01f ����%s',$d->{'magnitude'},$d->{'shindo'});
		$msg = $warnmsg.$times.' ('.$eqedmsg.'ȯ��)'.$center.$magnitude;
	}	
	
	my @notice_ch = keys %channel_noticeall;
#	push(@notice_ch,keys %channel_noticelimited) if($d->{'warn_num'} > 900 ||  $d->{'warn_num'} == 1 || $d->{'msg_type_code'} == 10 );
	if($d->{eq_id} ne $last_eq_id || $d->{msg_type_code} == 10){
		$last_eq_id = $d->{eq_id};
		push(@notice_ch,keys %channel_noticelimited);
	}

	print "+++>>[$msg]\n";
	foreach my $ch (@notice_ch){
		print "++Noticed [$ch]\n";
		Encode::from_to($msg,'euc-jp',$yaml->{irc}{charset});
		$ircsock->notice($ch,$msg);
	}

	if(-e "$eewlog/$tmpfname"){
		my $newname = $d->{eq_id}.'.'.$d->{warn_num};
		print "++Rename [$tmpfname] -> [$newname]\n";
		rename "$eewlog/$tmpfname" , "$eewlog/$newname";
	}
	
}

sub conv_charset
{
        my $d = shift;
        foreach my $key(keys %$d){
                $d->{$key} = Encode::encode('euc-jp',$d->{$key});
        }
}

